package flatten

import (
	"github.com/MarkRosemaker/errpath"
	"github.com/MarkRosemaker/openapi"
)

func operationResponses(d *openapi.Document, rs openapi.OperationResponses, opID string) error {
	for code, r := range rs.ByIndex() {
		modeSchema := alwaysMove
		if code.IsSuccess() {
			modeSchema = moveIfNecessary
		}

		if err := responseRef(d, r, nameResponse(opID, code), modeSchema); err != nil {
			return &errpath.ErrKey{Key: string(code), Err: err}
		}
	}

	return nil
}

func responses(d *openapi.Document, rs openapi.ResponsesByName) error {
	for name, r := range rs.ByIndex() {
		// NOTE: We are *not* calling responseRef here,
		// because we are calling this function from Components,
		// where the response should already be.
		modeSchema := moveIfNecessary
		if isFailureResponse(d, r) {
			modeSchema = alwaysMove
		}

		if err := response(d, r.Value, name, modeSchema); err != nil {
			return &errpath.ErrKey{Key: string(name), Err: err}
		}
	}

	return nil
}

func isFailureResponse(d *openapi.Document, r *openapi.ResponseRef) bool {
	for _, p := range d.Paths {
		for _, o := range p.Operations {
			for code, rs := range o.Responses {
				if rs == r && !code.IsSuccess() {
					return false
				}
			}
		}
	}

	return true
}
