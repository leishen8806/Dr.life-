package secrets

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"
)

const (
	formatVersion = byte(1)
	nonceSize     = 12
)

var (
	magic             = []byte("DLSE")
	ErrInvalidBlob    = errors.New("secrets: invalid encrypted value")
	ErrAuthentication = errors.New("secrets: authentication failed")
)

type Envelope struct {
	keys KeyProvider
}

type payload struct {
	keyID          string
	wrapNonce      []byte
	wrappedDEK     []byte
	dataNonce      []byte
	dataCiphertext []byte
}

func NewEnvelope(keys KeyProvider) (*Envelope, error) {
	if keys == nil {
		return nil, ErrInvalidKeyConfiguration
	}
	return &Envelope{keys: keys}, nil
}

func (e *Envelope) Encrypt(ctx context.Context, plaintext []byte, aad string) ([]byte, error) {
	keyID, kek, err := e.keys.ActiveKey(ctx)
	if err != nil {
		return nil, err
	}
	defer zero(kek)
	dek := make([]byte, KeySize)
	if _, err := rand.Read(dek); err != nil {
		return nil, errors.New("secrets: random source failed")
	}
	defer zero(dek)

	wrappedNonce, wrapped, err := seal(kek, dek, wrappingAAD(keyID, aad))
	if err != nil {
		return nil, err
	}
	dataNonce, ciphertext, err := seal(dek, plaintext, dataAAD(aad))
	if err != nil {
		return nil, err
	}
	return marshalPayload(payload{keyID, wrappedNonce, wrapped, dataNonce, ciphertext})
}

func (e *Envelope) Decrypt(ctx context.Context, blob []byte, aad string) ([]byte, error) {
	p, err := parsePayload(blob)
	if err != nil {
		return nil, err
	}
	kek, err := e.keys.KeyByID(ctx, p.keyID)
	if err != nil {
		return nil, err
	}
	defer zero(kek)
	dek, err := open(kek, p.wrapNonce, p.wrappedDEK, wrappingAAD(p.keyID, aad))
	if err != nil {
		return nil, ErrAuthentication
	}
	defer zero(dek)
	plaintext, err := open(dek, p.dataNonce, p.dataCiphertext, dataAAD(aad))
	if err != nil {
		return nil, ErrAuthentication
	}
	return plaintext, nil
}

func (e *Envelope) Rewrap(ctx context.Context, blob []byte, aad string) ([]byte, error) {
	p, err := parsePayload(blob)
	if err != nil {
		return nil, err
	}
	oldKEK, err := e.keys.KeyByID(ctx, p.keyID)
	if err != nil {
		return nil, err
	}
	dek, err := open(oldKEK, p.wrapNonce, p.wrappedDEK, wrappingAAD(p.keyID, aad))
	zero(oldKEK)
	if err != nil {
		return nil, ErrAuthentication
	}
	defer zero(dek)
	newID, newKEK, err := e.keys.ActiveKey(ctx)
	if err != nil {
		return nil, err
	}
	defer zero(newKEK)
	p.wrapNonce, p.wrappedDEK, err = seal(newKEK, dek, wrappingAAD(newID, aad))
	if err != nil {
		return nil, err
	}
	p.keyID = newID
	return marshalPayload(p)
}

func seal(key, plaintext, aad []byte) ([]byte, []byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, errors.New("secrets: random source failed")
	}
	return nonce, gcm.Seal(nil, nonce, plaintext, aad), nil
}

func open(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil || len(nonce) != gcm.NonceSize() {
		return nil, ErrAuthentication
	}
	return gcm.Open(nil, nonce, ciphertext, aad)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, ErrInvalidKeyConfiguration
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrInvalidKeyConfiguration
	}
	return cipher.NewGCM(block)
}

func wrappingAAD(keyID, aad string) []byte {
	return []byte("drlife:envelope:v1:kek:" + keyID + "\x00" + aad)
}

func dataAAD(aad string) []byte {
	return []byte("drlife:envelope:v1:data\x00" + aad)
}

func marshalPayload(p payload) ([]byte, error) {
	if !validKeyID(p.keyID) || len(p.wrapNonce) != nonceSize || len(p.dataNonce) != nonceSize || len(p.wrappedDEK) > 65535 || uint64(len(p.dataCiphertext)) > uint64(^uint32(0)) {
		return nil, ErrInvalidBlob
	}
	var out bytes.Buffer
	out.Write(magic)
	out.WriteByte(formatVersion)
	out.WriteByte(byte(len(p.keyID)))
	out.WriteString(p.keyID)
	out.Write(p.wrapNonce)
	_ = binary.Write(&out, binary.BigEndian, uint16(len(p.wrappedDEK)))
	out.Write(p.wrappedDEK)
	out.Write(p.dataNonce)
	_ = binary.Write(&out, binary.BigEndian, uint32(len(p.dataCiphertext)))
	out.Write(p.dataCiphertext)
	return out.Bytes(), nil
}

func parsePayload(blob []byte) (payload, error) {
	r := bytes.NewReader(blob)
	prefix := make([]byte, len(magic))
	if _, err := io.ReadFull(r, prefix); err != nil || !bytes.Equal(prefix, magic) {
		return payload{}, ErrInvalidBlob
	}
	version, err := r.ReadByte()
	if err != nil || version != formatVersion {
		return payload{}, ErrInvalidBlob
	}
	keyIDLen, err := r.ReadByte()
	if err != nil || keyIDLen == 0 || keyIDLen > 64 {
		return payload{}, ErrInvalidBlob
	}
	p := payload{keyID: string(readExact(r, int(keyIDLen)))}
	if !validKeyID(p.keyID) {
		return payload{}, ErrInvalidBlob
	}
	p.wrapNonce = readExact(r, nonceSize)
	var wrappedLen uint16
	if len(p.wrapNonce) != nonceSize || binary.Read(r, binary.BigEndian, &wrappedLen) != nil || wrappedLen < KeySize+16 {
		return payload{}, ErrInvalidBlob
	}
	p.wrappedDEK = readExact(r, int(wrappedLen))
	p.dataNonce = readExact(r, nonceSize)
	var dataLen uint32
	if len(p.wrappedDEK) != int(wrappedLen) || len(p.dataNonce) != nonceSize || binary.Read(r, binary.BigEndian, &dataLen) != nil || uint64(dataLen) > uint64(r.Len()) {
		return payload{}, ErrInvalidBlob
	}
	p.dataCiphertext = readExact(r, int(dataLen))
	if len(p.dataCiphertext) != int(dataLen) || r.Len() != 0 {
		return payload{}, ErrInvalidBlob
	}
	return p, nil
}

func readExact(r *bytes.Reader, size int) []byte {
	if size < 0 || size > r.Len() {
		return nil
	}
	value := make([]byte, size)
	if _, err := io.ReadFull(r, value); err != nil {
		return nil
	}
	return value
}
