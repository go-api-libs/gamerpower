package flatten

import (
	"github.com/MarkRosemaker/errpath"
	"github.com/MarkRosemaker/openapi"
)

func content(d *openapi.Document, c openapi.Content,
	rspOrReqBodyName, tp string, modeSchema mode,
) error {
	for mr, mt := range c.ByIndex() {
		if err := mediaType(d, mt,
			nameMediaType(rspOrReqBodyName, nameMediaRange(mr), tp),
			modeSchema); err != nil {
			return &errpath.ErrKey{Key: string(mr), Err: err}
		}
	}

	return nil
}
