package main

//go:generate go tool openapi-enrich -trim-examples=20
//go:generate go tool openapi-flatten
//go:generate go tool openapi-compress -trim-examples=3
//go:generate go tool openapi-codegen -client
