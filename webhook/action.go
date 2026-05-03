package webhook

import "context"

type Action interface {
	Execute(ctx context.Context, payloadCtx PayloadContext, body []byte) error
}
