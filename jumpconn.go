package main

/*
 * jumpconn.go
 * Make connections between the jumps
 * By J. Stuart McMurray
 * Created 20170401
 * Last Modified 20170401
 */

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

/* DEFPORT is the default SSH port */
const DEFPORT = "22"

/* makeSSHConns returs a list of ssh clients, of which each subsequent client
connected to its server through the previous one (except the first one, of
course).  It attempts to use the jumps in jumps in order, and will make njump
connections, or use all the jumps if njump is zero.  If there's fewer working
jumps than njump, all the connections are disconnected and an error is
returned.  The context is checked before every connection attempt for an
indication to stop.  Once the final jump has been established, a connection to
exitTest is made to test for connectivity. */
func MakeSSHConns(
	ctx context.Context,
	jumps []jump,
	njump uint,
	connto time.Duration,
	hsto time.Duration,
	kaint time.Duration,
	exitTest string,
	cancel context.CancelFunc,
) ([]*ssh.Client, error) {
	var (
		d  Dialer = &net.Dialer{}
		cs []*ssh.Client
	)
	for _, j := range jumps {
		/* Make sure we're not meant to quit yet */
		if nil != ctx.Err() {
			CloseJumps(cs)
			return nil, fmt.Errorf("interrupt")
		}
		cstr := fmt.Sprintf( /* Connection string */
			"%v@%v %v (%v)",
			j.username,
			j.host,
			j.password,
			j.version,
		)
		/* Make sure the address has a port */
		_, p, err := net.SplitHostPort(j.host)
		if "" == p || nil != err {
			j.host = net.JoinHostPort(j.host, DEFPORT)
		}
		/* Dial with the previous conn as the dialer */
		c, err := dialWithTimeout(ctx, d, j.host, connto)
		if nil != err {
			/* Handle case in which the jump doesn't forward
			connections */
			if isSSHForwardErr(err) {
				log.Printf(
					"Jump %v does not allow connection "+
						"forwarding, closing",
					len(cs),
				)
				d, cs = removeLastJump(cs)
				continue
			}
			log.Printf(
				"Unable to connect to %v: %v",
				j.host,
				err,
			)
			continue
		}

		worky := make(chan struct{}) /* Will be closed on handshake */
		var aberr error
		/* Kill the connection if the handshake takes too long */
		go func() {
			select {
			case <-ctx.Done():
				c.Close()
				aberr = fmt.Errorf("interrupt")
			case <-time.After(hsto):
				c.Close()
				aberr = fmt.Errorf("timeout")
			case <-worky:
			}
		}()
		/* Keyboard-interactive auth function */
		ki := func(
			user string,
			instruction string,
			questions []string,
			echos []bool,
		) (answers []string, err error) {
			return []string{j.password}, nil
		}
		/* Upgrade to an SSH connection */
		scon, chans, reqs, err := ssh.NewClientConn(
			c,
			j.host,
			&ssh.ClientConfig{
				User: j.username,
				Auth: []ssh.AuthMethod{
					ssh.Password(j.password),
					ssh.KeyboardInteractive(ki),
				},
				ClientVersion: j.version,
			},
		)
		if nil != err {
			/* Change the error if it was a timeout */
			if nil != aberr {
				err = aberr
			}
			log.Printf(
				"Unable to handshake as %v: %v",
				cstr,
				err,
			)
			c.Close()
			continue
		}
		/* Don't timeout the handshake */
		close(worky)

		/* Upgrade to an SSH client */
		scli := ssh.NewClient(scon, chans, reqs)

		/* Add it to the list of connections */
		cs = append(cs, scli)
		log.Printf(
			"Jump %v: %v",
			len(cs),
			cstr,
		)

		/* If we have enough, we're done */
		if uint(0) != njump && uint(len(cs)) >= njump {
			/* Make sure we can proxy through the last jump */
			if testExit(cs[len(cs)-1], exitTest) {
				go sendKeepalives(cs[len(cs)-1], kaint, cancel)
				return cs, nil
			}
			d, cs = removeLastJump(cs)
			continue
		}

		/* Next dialer is the previous jump */
		d = scli
	}
	if nil != ctx.Err() {
		return nil, fmt.Errorf("interrupt")
	}
	/* If we ran out of jumps, tear down what we have */
	if uint(0) != njump && uint(len(cs)) < njump {
		CloseJumps(cs)
		return nil, fmt.Errorf(
			"insufficient SSH jumps (only made %v/%v)",
			len(cs),
			njump,
		)
	}
	if uint(0) != njump {
		log.Printf("Out of jumps, made %v / %v", len(cs), njump) /* DEBUG */
		log.Printf("This is a bug, please tell the dev")         /* DEBUG */
	}
	/* Make sure we can get out */
	if testExit(cs[len(cs)-1], exitTest) {
		return cs, nil
	}
	/* If we're here, we failed to exittest */
	if 1 == len(cs) {
		return nil, fmt.Errorf("no working jumps found")
	}
	log.Printf("Closing last jump")
	_, cs = removeLastJump(cs)
	go sendKeepalives(cs[len(cs)-1], kaint, cancel)
	return cs, nil
}

/* CloseJumps closes the slice of SSH connections, starting with the highest
index (i.e. len(cs)-1). */
func CloseJumps(cs []*ssh.Client) {
	if 0 == len(cs) {
		return
	}
	for i := len(cs) - 1; i >= 0; i-- {
		err := cs[i].Close()
		if nil != err {
			log.Printf("Unable to close jump %v: %v", i+1, err)
			continue
		}
		log.Printf("Closed jump %v", i+1)
	}
}

/* dialWithTimeout dials a via d, but gives up after to or if ctx signals to
finish. */
func dialWithTimeout(
	ctx context.Context,
	d Dialer,
	a string,
	to time.Duration,
) (net.Conn, error) {
	ech := make(chan error)
	worky := make(chan net.Conn)
	/* Dial the address */
	go func() {
		c, err := d.Dial("tcp", a)
		/* When it's done, send the result back */
		ech <- err
		worky <- c
	}()
	/* Wait for something to happen */
	select {
	case <-time.After(to):
		go killConnFromChannel(worky)
		return nil, fmt.Errorf("timeout")
	case <-ctx.Done():
		go killConnFromChannel(worky)
		return nil, fmt.Errorf("interrupt")
	case err := <-ech:
		/* If there was no error, return it */
		if nil == err {
			return <-worky, nil
		}
		go killConnFromChannel(worky)
		return nil, err
	}
}

/* killConnFromChannel reads a conn from ch and calls its Close method */
func killConnFromChannel(ch <-chan net.Conn) {
	c := <-ch
	if nil != c {
		c.Close()
	}
}

/* testExit returns true if a connection was able to be made to the target via
the client. */
func testExit(sc *ssh.Client, target string) bool {
	log.Printf("Making a test connection to %v", target)
	c, err := sc.Dial("tcp", target)
	if nil != err {
		log.Printf("Connection to %v failed: %v", target, err)
		return false
	}
	log.Printf("Connection to %v successful", target)
	c.Close()
	return true
}

/* isSSHForwardError returns true if the error indicates that an SSH server
won't likely forward things for us. */
func isSSHForwardErr(err error) bool {
	/* Maybe prohibited */
	e, ok := err.(*ssh.OpenChannelError)
	if ok {
		return ssh.Prohibited == e.Reason
	}
	/* SSH Weirdness */
	if strings.HasSuffix(
		err.Error(),
		"ssh: unexpected packet in response to channel open: <nil>",
	) {
		return true
	}
	return false
}

/* sendKeepalives sends keepalives on the ssh connection at the given
interval*/
func sendKeepalives(
	c *ssh.Client,
	interval time.Duration,
	cancel context.CancelFunc,
) {
	log.Printf("Sending keepalives every %v to last jump", interval)
	for {
		if _, _, err := c.SendRequest(
			"keepalive@openssh.com",
			true,
			nil,
		); err != nil {
			log.Printf("No longer seending keepalives: %v", err)
			break
		}
		time.Sleep(interval)
	}
	cancel()
}

/* removeLastJump closes and removes the last jump from cs and returns the
dialer to find the next jump. */
func removeLastJump(cs []*ssh.Client) (Dialer, []*ssh.Client) {
	/* Close the bad last jump */
	err := cs[len(cs)-1].Close()
	if nil != err {
		log.Printf("Unable to close jump %v: %v", len(cs), err)
	}
	/* Remove it from the list */
	cs = cs[:len(cs)-1]
	/* Work out the next dialer */
	if 0 == len(cs) {
		return &net.Dialer{}, cs
	}
	return cs[len(cs)-1], cs
}
