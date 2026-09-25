package enrich

import (
	"github.com/MarkRosemaker/openapi"
)

// hoistSecurity moves security requirements that appear on every operation to
// the document level, then removes them from individual operations.
//
// This produces cleaner specs: instead of repeating `security: [{bearerAuth: []}]`
// on every operation, the requirement is stated once at the top of the document
// and inherited by all operations.
//
// A requirement is hoisted only if it appears on ALL operations. Operations
// with an explicit empty security slice (`security: []`) count as "no security
// required for this operation" and prevent hoisting, preserving the intentional
// override semantics defined by the OpenAPI spec.
func hoistSecurity(doc *openapi.Document) {
	// Collect all operations.
	var ops []*openapi.Operation
	for _, pi := range doc.Paths {
		for _, op := range pi.Operations {
			ops = append(ops, op)
		}
	}

	if len(ops) == 0 {
		return
	}

	// Gather the union of security requirements across every operation.
	var candidates []openapi.SecurityRequirement
	for _, op := range ops {
		for _, req := range op.Security {
			if !containsReq(candidates, req) {
				candidates = append(candidates, req)
			}
		}
	}

	// Hoist each requirement that is present on every single operation.
	for _, req := range candidates {
		if allOpsHave(ops, req) {
			doc.Security = append(doc.Security, req)
			for _, op := range ops {
				op.Security = removeReq(op.Security, req)
			}
		}
	}
}

// allOpsHave reports whether every operation contains req in its security list.
func allOpsHave(ops []*openapi.Operation, req openapi.SecurityRequirement) bool {
	for _, op := range ops {
		if !op.Security.Contains(req) {
			return false
		}
	}

	return true
}

// removeReq returns reqs without req; returns nil if the result is empty.
func removeReq(reqs openapi.SecurityRequirements, req openapi.SecurityRequirement) openapi.SecurityRequirements {
	out := reqs[:0:0]
	for _, r := range reqs {
		if !r.Equals(req) {
			out = append(out, r)
		}
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

// containsReq reports whether reqs already contains req.
func containsReq(reqs []openapi.SecurityRequirement, req openapi.SecurityRequirement) bool {
	for _, r := range reqs {
		if r.Equals(req) {
			return true
		}
	}

	return false
}
