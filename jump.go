package main

/*
 * jump.go
 * Reads the jumps from the jumpfile
 * By J. Stuart McMurray
 * Created 20170401
 * Last Modified 20170401
 */

import (
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/crypto/ssh"
)

/* KEYPREFIX is the password prefix to indicate a keyfile */
const KEYPREFIX = "key:"

/* JUMPRE parses lines of the jumpfile */
var JUMPRE = regexp.MustCompile(`^([^@]+)@(\S+)\s+(.*)\s(SSH-\S+)$`)

/* jump represents an entry in the jumpfile */
type jump struct {
	username string
	host     string
	password string
	version  string
	key      ssh.Signer
}

/* ReadJumps reads the jumpfile and returns the jumps */
func ReadJumps(fname string, keydir string) ([]jump, error) {
	/* Slurp the jumpfile */
	jf, err := ioutil.ReadFile(fname)
	if nil != err {
		return nil, err
	}

	/* Split into lines */
	ls := strings.Split(string(jf), "\n")

	/* Parse into jumps */
	var js []jump
	for _, l := range ls {
		l = strings.TrimSpace(l)
		/* Ignore blanks and comments */
		if "" == l || strings.HasPrefix(l, "#") {
			continue
		}
		/* Grow the list of jumps */
		ms := JUMPRE.FindStringSubmatch(l)
		if nil == ms {
			log.Printf("Invalid line in jump file: %q", l)
		}
		j := jump{
			username: ms[1],
			host:     ms[2],
			password: ms[3],
			version:  ms[4],
		}
		/* Handle a possible key */
		if strings.HasPrefix(j.password, KEYPREFIX) {
			kf := strings.TrimPrefix(
				j.password,
				KEYPREFIX,
			)
			key, err := getKey(keydir, kf)
			if nil != err {
				log.Printf(
					"Unable to retreive key for "+
						"%v@%v from %v: %v",
					j.username,
					j.host,
					kf,
					err,
				)
			} else {
				j.key = key
			}
		}
		/* Add it to the list */
		js = append(js, j)
		continue
	}
	if 0 == len(js) {
		return nil, fmt.Errorf("no jumps in %v", fname)
	}

	return js, nil
}

/* shuffleJumps shuffles a slice of jumps */
func ShuffleJumps(s []jump) {
	for i := range s {
		j := rand.Intn(i + 1)
		s[i], s[j] = s[j], s[i]
	}
}

/* getKey tries to get the key named keyname.  If it is a relative path, it
will be searched for in keydir. */
func getKey(keydir, keyfile string) (ssh.Signer, error) {
	/* Work out where the file should be */
	if !filepath.IsAbs(keyfile) {
		keyfile = filepath.Join(keydir, keyfile)
	}
	/* Slurp the file */
	b, err := ioutil.ReadFile(keyfile)
	if nil != err {
		return nil, err
	}
	/* Turn it into a signer */
	s, err := ssh.ParsePrivateKey(b)
	return s, err
}
