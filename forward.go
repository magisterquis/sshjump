package main

/*
 * forward.go
 * Handle forwarding of connections
 * By J. Stuart McMurray
 * Created 20170401
 * Last Modified 20170401
 */

import (
	"fmt"
	"io"
	"log"
	"net"
	"regexp"
	"sync"

	"golang.org/x/crypto/ssh"
)

/* FWDRE parses forwarding specifications */
var FWDRE = regexp.MustCompile(`^(L|R)([^,]+),(\d+),([^,]+),(\d+)$`)

/* fwdspec holds a specification for a forward */
type fwdspec struct {
	isFwd bool   /* True for L, false for R */
	laddr string /* Listen address */
	caddr string /* Connect address */
}

/* ParseForwards parses the forwarding specifications on the command line */
func ParseForwards(specs []string) []fwdspec {
	fs := make([]fwdspec, 0)
	for _, s := range specs {
		ms := FWDRE.FindStringSubmatch(s)
		if nil == ms {
			log.Fatalf("Invalid forwarding specification %q", s)
		}
		fs = append(fs, fwdspec{
			isFwd: "L" == ms[1],
			laddr: net.JoinHostPort(ms[2], ms[3]),
			caddr: net.JoinHostPort(ms[4], ms[5]),
		})
	}
	return fs
}

/* CloseListeners closes the listeners in ls. */
func CloseListeners(ls []net.Listener) {
	for _, l := range ls {
		if err := l.Close(); nil != err {
			log.Printf(
				"Unable to close listener %v: %v",
				l.Addr(),
				err,
			)
		}
		log.Printf("Closed listener %v", l.Addr())
	}
}

/* ForwardPorts parses the list of forwards proxies connections via the ssh
connection according to the forwards.  Fatal errors encountered during
proxying will be sent back on errChan. */
func ForwardPorts(
	c *ssh.Client,
	forwards []fwdspec,
	errChan chan<- error,
) ([]net.Listener, error) {
	var (
		ls  []net.Listener
		err error
	)
	/* Try to listen on each of the forwarded ports */
	for _, f := range forwards {
		var (
			l net.Listener
			d Dialer
		)
		/* Listen */
		if f.isFwd {
			l, err = net.Listen("tcp", f.laddr)
			d = c
		} else {
			l, err = c.Listen("tcp", f.laddr)
			d = &net.Dialer{}
		}
		if nil != err {
			/* On error, close all of the other listeners */
			CloseListeners(ls)
			return nil, err
		}
		/* Fire off a handler */
		go forwardPort(l, d, f, errChan)
		dir := "forward"
		if !f.isFwd {
			dir = "reverse"
		}
		log.Printf(
			"Listening on %v for %v connections to %v",
			l.Addr(),
			dir,
			f.caddr,
		)
		ls = append(ls, l)
	}
	return ls, err
}

/* forwardPort accepts clients on l and forwards to f.caddr via d.  Fatal
errors will be sent to ec */
func forwardPort(l net.Listener, d Dialer, f fwdspec, ec chan<- error) {
	/* Accept clients and proxy */
	for {
		/* Pop off a client */
		c, err := l.Accept()
		if nil != err {
			ec <- err
			return
		}
		/* Handle */
		go forwardConnection(c, d, f)
	}
}

/* forwardConnection proxies the connection t to a connection made to f.caddr
via d. */
func forwardConnection(ic net.Conn, d Dialer, f fwdspec) {
	RegisterConn(ic)
	defer CloseConn(ic)
	/* Attempt to connect to the target */
	oc, err := d.Dial("tcp", f.caddr)
	if nil != err {
		var cs string
		if f.isFwd {
			cs = fmt.Sprintf("%v->%v", ic.RemoteAddr(), f.caddr)
		} else {
			cs = fmt.Sprintf("%v<-%v", f.caddr, ic.RemoteAddr())
		}
		log.Printf(
			"Unable to forward connection %v: %v",
			cs,
			err,
		)
		return
	}
	RegisterConn(oc)
	defer CloseConn(oc)
	var cs string
	if f.isFwd {
		cs = fmt.Sprintf("%v->%v", ic.RemoteAddr(), f.caddr)
	} else {
		cs = fmt.Sprintf("%v<-%v", f.caddr, ic.RemoteAddr())
	}
	log.Printf("Begin %v", cs)

	/* Proxy bytes */
	var (
		ltrn int64
		ltre error
		rtln int64
		rtle error
	)
	wg := &sync.WaitGroup{}
	wg.Add(2)

	if f.isFwd {
		go proxy(oc, ic, &ltrn, &ltre, wg)
		go proxy(ic, oc, &rtln, &rtle, wg)
	} else {
		go proxy(oc, ic, &rtln, &rtle, wg)
		go proxy(ic, oc, &ltrn, &ltre, wg)
	}

	wg.Wait()
	log.Printf(
		"End %v LtRBytes:%v LtRErr:%v RtLBytes:%v RtLErr:%v",
		cs,
		ltrn,
		ltre,
		rtln,
		rtle,
	)

}

/* proxy copies bytes from src to dst.  On completion, wg's Done method is
called, and the number of bytes copied and any error encountered are put in n
and err. */
func proxy(
	dst io.Writer,
	src io.Reader,
	n *int64,
	err *error,
	wg *sync.WaitGroup,
) {
	defer wg.Done()
	*n, *err = io.Copy(dst, src)
}
