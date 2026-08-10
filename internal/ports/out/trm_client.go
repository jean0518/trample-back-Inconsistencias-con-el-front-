package out

import "context"

type TRMClient interface {
	GetRate(ctx context.Context) (float64, error)
}
