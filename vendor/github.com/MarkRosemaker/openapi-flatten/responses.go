package flatten

import (
	"github.com/MarkRosemaker/errpath"
	"github.com/MarkRosemaker/openapi"
)

func operationResponses(d *openapi.Document, rs openapi.OperationResponses, opID string) error {
	for code, r := range rs.ByIndex() {
		alwaysMove := !code.IsSuccess()

		if err := responseRef(d, r, nameResponse(opID, code), alwaysMove); err != nil {
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
		alwaysMove := isFailureResponse(d, name)

		if err := response(d, r.Value, name, alwaysMove); err != nil {
			return &errpath.ErrKey{Key: string(name), Err: err}
		}
	}

	return nil
}

// isFailureResponse reports whether an operation uses the component response named name for a status other than a success.
func isFailureResponse(d *openapi.Document, name string) bool {
	ref := newRef("responses", name).Identifier

	for _, p := range d.Paths {
		for _, o := range p.Operations {
			for code, rs := range o.Responses {
				if !code.IsSuccess() && rs.Ref != nil && rs.Ref.Identifier == ref {
					return true
				}
			}
		}
	}

	return false
}
