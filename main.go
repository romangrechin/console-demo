/*
Copyright 2015 Gravitational, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package main

import (
	"io"
	"log"
	"net/http"
	"os/exec"
	"strings"

	"github.com/kr/pty"
	"golang.org/x/net/websocket"
	"os"
	"strconv"
)

const (
	listenAddr = "0.0.0.0:5000"
)

type Handler struct {
	fileServer http.Handler
}

func main() {
	http.ListenAndServe(listenAddr, &Handler{
		fileServer: http.FileServer(assetFS()),
	})
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// need to serve shell via websocket?
	if strings.Trim(r.URL.Path, "/") == "shell" {
		onShell(w, r)
		return
	}
	// serve static assets from 'static' dir:
	h.fileServer.ServeHTTP(w, r)
}

// GET /shell handler
// Launches /bin/bash and starts serving it via the terminal
func onShell(w http.ResponseWriter, r *http.Request) {
	cols, _ := strconv.Atoi(r.FormValue("cols"))
	rows, _ := strconv.Atoi(r.FormValue("rows"))

	wsHandler := func(ws *websocket.Conn) {
		var tty *os.File
		var err error
		// wrap the websocket into UTF-8 wrappers:
		wrapper := NewWebSockWrapper(ws, WebSocketTextMode)
		stdout := wrapper
		stderr := wrapper

		// this one is optional (solves some weird issues with vim running under shell)
		stdin := &InputWrapper{ws}

		// starts new command in a newly allocated terminal:
		// TODO: replace /bin/bash with:
		//		 kubectl exec -ti <pod> --container <container name> -- /bin/bash
		cmd := exec.Command("/bin/bash")

		if cols > 0 && rows > 0 {
			tty, err = pty.StartWithSize(cmd, &pty.Winsize{
				Cols: uint16(cols),
				Rows: uint16(rows),
			})
		} else {
			tty, err = pty.Start(cmd)
		}

		if err != nil {
			panic(err)
		}
		defer tty.Close()

		// pipe to/fro websocket to the TTY:
		go func() {
			io.Copy(stdout, tty)
		}()
		go func() {
			io.Copy(stderr, tty)
		}()
		go func() {
			io.Copy(tty, stdin)
		}()

		// wait for the command to exit, then close the websocket
		cmd.Wait()
	}
	defer log.Printf("Websocket session closed for %v", r.RemoteAddr)

	// start the websocket session:
	websocket.Handler(wsHandler).ServeHTTP(w, r)
}
