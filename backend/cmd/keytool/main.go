package main

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	platformsecrets "github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
)

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "keytool: operation failed")
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	if len(args) != 1 {
		return errors.New("usage: keytool generate|split|combine")
	}
	switch args[0] {
	case "generate":
		key := make([]byte, platformsecrets.KeySize)
		if _, err := rand.Read(key); err != nil {
			return err
		}
		defer wipe(key)
		fmt.Fprintln(stderr, "This key is shown once. Store it offline.")
		fmt.Fprintln(stdout, base64.StdEncoding.EncodeToString(key))
		return nil
	case "split":
		key, err := readBase64Line(stdin)
		if err != nil {
			return err
		}
		defer wipe(key)
		shareA, shareB, err := platformsecrets.SplitKey(key)
		if err != nil {
			return err
		}
		defer wipe(shareA)
		defer wipe(shareB)
		fmt.Fprintln(stderr, "Enter one base64 master key on standard input. Give each output line to a different administrator.")
		fmt.Fprintln(stdout, base64.StdEncoding.EncodeToString(shareA))
		fmt.Fprintln(stdout, base64.StdEncoding.EncodeToString(shareB))
		return nil
	case "combine":
		scanner := bufio.NewScanner(stdin)
		shareA, err := scanBase64(scanner)
		if err != nil {
			return err
		}
		defer wipe(shareA)
		shareB, err := scanBase64(scanner)
		if err != nil {
			return err
		}
		defer wipe(shareB)
		key, err := platformsecrets.CombineShares(shareA, shareB)
		if err != nil {
			return err
		}
		defer wipe(key)
		fmt.Fprintln(stderr, "Combined key is shown once. Avoid routine reconstruction.")
		fmt.Fprintln(stdout, base64.StdEncoding.EncodeToString(key))
		return nil
	default:
		return errors.New("unknown command")
	}
}

func readBase64Line(reader io.Reader) ([]byte, error) {
	scanner := bufio.NewScanner(reader)
	return scanBase64(scanner)
}

func scanBase64(scanner *bufio.Scanner) ([]byte, error) {
	if !scanner.Scan() {
		return nil, errors.New("missing standard input")
	}
	value, err := base64.StdEncoding.DecodeString(strings.TrimSpace(scanner.Text()))
	if err != nil {
		return nil, errors.New("invalid standard input")
	}
	return value, nil
}

func wipe(value []byte) {
	for i := range value {
		value[i] = 0
	}
}
