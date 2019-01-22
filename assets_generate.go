// +build ignore

package main

import (
	"log"
	"net/http"

	"github.com/shurcooL/vfsgen"
)

func main() {
	fs := http.Dir("assets")
	if err := vfsgen.Generate(fs, vfsgen.Options{
		PackageName:  "main",
		BuildTags:    "!dev",
		VariableName: "assets",
	}); err != nil {
		log.Fatalln(err)
	}
}
