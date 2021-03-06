package main

/*
 * sshjump.go
 * Jump through a few SSH hosts
 * By J. Stuart McMurray
 * Created 20170305
 * Last Modified 20170331
 */

import (
	"context"
	crand "crypto/rand"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	mrand "math/rand"
	"net"
	"os"
	"os/signal"
	"time"
)

/* Dialer is anything which can dial */
type Dialer interface {
	Dial(network, addr string) (c net.Conn, err error)
}

func main() {
	var (
		jumpfile = flag.String(
			"jumps",
			"",
			"Name of `file` containing SSH jumps",
		)
		njump = flag.Uint(
			"njump",
			5,
			"The first `N` working jumps in the jumpfile will be "+
				"used, or 0 to use all of the jumps",
		)
		shuffle = flag.Bool(
			"shuffle",
			false,
			"Shuffle the list of jumps",
		)
		hsto = flag.Duration(
			"hsto",
			15*time.Second,
			"SSH handshake `timeout`",
		)
		connto = flag.Duration(
			"connto",
			10*time.Second,
			"TCP connection `timeout`",
		)
		kaint = flag.Duration(
			"kaint",
			time.Second,
			"SSH keepalive `interval`",
		)
		exitTest = flag.String(
			"exittest",
			"check.torproject.org:443",
			"Host and port on `target` to test last "+
				"jump forwarding ability",
		)
		keyDir = flag.String(
			"keydir",
			".",
			"Top-level directory for keys with a "+
				"non-absolute path",
		)
	)
	flag.Usage = func() {
		fmt.Fprintf(
			os.Stderr,
			`Usage: %v [options] fwdspec [fwdspec...]

The jumpfile must contain lines of the form
user@host password versionstring

If the password is of the form %vfilename, it is taken to be used as the name
of a PEM-encoded SSH key (e.g. generated by ssh-keygen).  If the file cannot
be found, it is assumed that it was actually a password starting with %v.

Each fwdspec should be of one of the following forms

L<laddr>,<lport>,<targetaddr>,<targetport>
R<raddr>,<rport>,<targetaddr>,<targetport>

The fwdspecs are similar to OpenSSH's -L and -R options, but always consist of
two address/port pairs.

Options:
`,
			os.Args[0],
			KEYPREFIX,
			KEYPREFIX,
		)
		flag.PrintDefaults()
	}
	flag.Parse()

	log.SetOutput(os.Stdout)

	/* Try to seed the random number generator */
	if err := seedRandom(); nil != err {
		log.Fatalf("Unable to seed PRNG with CSPRNG: %v", err)
	}

	/* Parse the forwarding specs */
	forwards := ParseForwards(flag.Args())
	if 0 == len(forwards) {
		fmt.Fprintf(os.Stderr, "No forwarding specifications given\n")
		os.Exit(1)
	}
	log.Printf("Parsed %v forwarding specifications", len(forwards))
	for i, f := range forwards {
		if f.isFwd {
			log.Printf("%v: %v -> %v", i, f.laddr, f.caddr)
		} else {
			log.Printf("%v: %v <- %v", i, f.caddr, f.laddr)
		}
	}

	/* Slurp the jumpfile */
	if "" == *jumpfile {
		log.Fatalf("No jumpfile given with -jumps")
	}
	jumps, err := ReadJumps(*jumpfile, *keyDir)
	if nil != err {
		log.Fatalf("Unable to read jumpfile: %v", err)
	}
	if 0 == len(jumps) {
		log.Fatalf("No useable jumps in jumpfile (%q)", *jumpfile)
	}
	log.Printf("Read %v jumps from %v", len(jumps), *jumpfile)

	/* Shuffle it if need be */
	if *shuffle {
		ShuffleJumps(jumps)
		log.Printf("Shuffled jump list")
	}

	/* Pass errors up and cancels down */
	ctx, cancel := context.WithCancel(context.Background())
	errChan := make(chan error)

	/* Watch for incoming sigints */
	sigChan := make(chan os.Signal)
	go func() {
		s := <-sigChan
		log.Printf("Caught %v, gracefully giving up", s)
		cancel()
		s = <-sigChan
		log.Printf("Caught %v, dying", s)
		os.Exit(1)
	}()

	signal.Notify(sigChan, os.Interrupt)

	/* Make connection to last node */
	log.Printf("Making SSH jumps")
	sshConns, err := MakeSSHConns(
		ctx,
		jumps,
		*njump,
		*connto,
		*hsto,
		*kaint,
		*exitTest,
		cancel,
	)
	if nil != err {
		log.Fatalf("Unable to make SSH connections: %v", err)
	}
	defer CloseJumps(sshConns)

	/* Attempt forwards on command line */
	listeners, err := ForwardPorts(
		sshConns[len(sshConns)-1],
		forwards,
		errChan,
	)
	if nil != err {
		log.Fatalf("Unable to forward ports: %v", err)
	}
	defer CloseConns()
	defer CloseListeners(listeners)

	/* Wait for something bad to happen */
	select {
	case <-ctx.Done():
		/* TODO: Print something useful */
	case err := <-errChan:
		log.Printf("Error: %v", err)
		cancel()
		/* TODO: Print something useful */
	}
}

/* seedRandom seeds the PRNG with an int64 from the CSPRNG */
func seedRandom() error {
	/* Get an int64 from the CSPRNG */
	b := make([]byte, 8)
	_, err := crand.Read(b)
	if nil != err {
		return err
	}
	i := binary.LittleEndian.Uint64(b)

	/* Use it to seed the PRNG */
	mrand.Seed(int64(i))

	return nil
}

/* TODO: Key auth */
