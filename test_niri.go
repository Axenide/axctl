package main
import (
	"fmt"
	"os"
	"path/filepath"
)
func main() {
    matches, _ := filepath.Glob("/run/user/1000/niri-*.sock")
    fmt.Println(matches)
}
