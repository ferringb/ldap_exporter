package main

import (
	"log"
	"net/http"

	"github.com/shurcooL/vfsgen"
)

func main() {
	templates := http.Dir("assets/definitions")
	if err := vfsgen.Generate(templates, vfsgen.Options{}); err != nil {
		log.Fatalln(err)
	}
}
