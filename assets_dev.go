// +build dev

package main

import "net/http"

// Assets contains project assets.
var assets http.FileSystem = http.Dir("assets")
