package ledger

import "context"

// PostForTest exposes the package-private posting primitive to external-package
// tests. It is compiled only into test binaries and is not part of production.
func PostForTest(ctx context.Context, service *Service, request PostRequest) (PostResult, error) {
	return service.postRaw(ctx, request, nil)
}
