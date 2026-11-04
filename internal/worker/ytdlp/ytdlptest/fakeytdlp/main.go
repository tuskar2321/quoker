// Command fakeytdlp is the scripted yt-dlp used by tests; see ytdlptest.
package main

import (
	"os"

	"github.com/tuskar2321/quoker/internal/worker/ytdlp/ytdlptest"
)

func main() { os.Exit(ytdlptest.Run(os.Args[1:])) }
