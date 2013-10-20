package main

import (
	"flag"
	"fmt"
	"github.com/epeli/hooktftp/config"
	"github.com/epeli/hooktftp/hooks"
	"github.com/epeli/hooktftp/tftp"
	"io"
	"io/ioutil"
	"net"
	"os"
	"time"
)

var HOOKS []hooks.Hook
var CONFIG_PATH string = "/etc/hooktftp.yml"

func handleRRQ(res *tftp.RRQresponse) {

	started := time.Now()

	path := res.Request.Path

	fmt.Println("GET", path, "blocksize", res.Request.Blocksize)

	if err := res.WriteOACK(); err != nil {
		fmt.Println("Failed to write OACK", err)
		return
	}

	var reader io.Reader
	for _, hook := range HOOKS {
		var err error
		reader, err = hook(res.Request.Path)
		if err == hooks.NO_MATCH {
			continue
		} else if err != nil {

			if err, ok := err.(*os.PathError); ok {
				res.WriteError(tftp.NOT_FOUND, err.Error())
				return
			}

			fmt.Printf("Failed to execute hook for '%v' error: %v", res.Request.Path, err)
			res.WriteError(tftp.UNKNOWN_ERROR, "Hook exec failed")
			return
		}
		break
	}

	if reader == nil {
		res.WriteError(tftp.NOT_FOUND, "No hook matches")
		return
	}

	// TODO: close!!

	b := make([]byte, res.Request.Blocksize)

	totalBytes := 0

	for {
		bytesRead, err := reader.Read(b)
		totalBytes += bytesRead

		if err == io.EOF {
			if _, err := res.Write(b[:bytesRead]); err != nil {
				fmt.Println("Failed to write last bytes of the reader", err)
				return
			}
			res.End()
			break
		} else if err != nil {
			fmt.Println("Error while reading", reader, err)
			res.WriteError(tftp.UNKNOWN_ERROR, err.Error())
			return
		}

		if _, err := res.Write(b[:bytesRead]); err != nil {
			fmt.Println("Failed to write bytes for", path, err)
			return
		}
	}

	took := time.Since(started)

	speed := float64(totalBytes) / took.Seconds() / 1024 / 1024

	fmt.Printf("Sent %v bytes in %v %f MB/s\n", totalBytes, took, speed)
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [config]\n", os.Args[0])
	}
	flag.Parse()

	if len(flag.Args()) > 0 {
		CONFIG_PATH = flag.Args()[0]
	}

	fmt.Println("Reading hooks from", CONFIG_PATH)

	configData, err := ioutil.ReadFile(CONFIG_PATH)

	if err != nil {
		fmt.Println("Failed to read config", err)
		return
	}

	conf, err := config.ParseYaml(configData)
	if err != nil {
		fmt.Println("Failed to parse config", err)
		return
	}

	for _, hookDef := range conf.HookDefs {
		fmt.Println("Compiling hook", hookDef)

		// Create new hookDef variable for the hookDef pointer for each loop
		// iteration. Go reuses the hookDef variable and if we pass pointer to
		// that terrible things happen.
		newPointer := hookDef
		hook, err := hooks.CompileHook(&newPointer)
		if err != nil {
			fmt.Println("Failed to compile hook", hookDef, err)
			return
		}
		HOOKS = append(HOOKS, hook)
	}

	addr, err := net.ResolveUDPAddr("udp", ":"+conf.Port)
	if err != nil {
		fmt.Println("Failed to resolve address", err)
		return
	}

	server, err := tftp.NewTFTPServer(addr)
	if err != nil {
		fmt.Println("Failed to listen", err)
		return
	}

	for {
		res, err := server.Accept()
		if err != nil {
			fmt.Println("Bad tftp request", err)
			continue
		}

		go handleRRQ(res)
	}

}