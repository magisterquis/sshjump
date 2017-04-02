package main

/*
 * forward.go
 * Handle forwarding of connections
 * By J. Stuart McMurray
 * Created 20170401
 * Last Modified 20170401
 */

import (
	"log"
	"net"
	"sync"
)

var conns = make(map[net.Conn]struct{})
var connL = &sync.Mutex{}

/* RegisterConn keeps hold of a conn so it can be closed before termination */
func RegisterConn(c net.Conn) {
	connL.Lock()
	defer connL.Unlock()
	conns[c] = struct{}{}
}

/* CloseConn closes the conn and removes it from the set to be closed on
termination */
func CloseConn(c net.Conn) error {
	connL.Lock()
	defer connL.Unlock()
	return closeConn(c)
}

/* CloseConns closes all the registered conns */
func CloseConns() {
	log.Printf("Closing proxied connections")
	connL.Lock()
	defer connL.Unlock()
	for k := range conns {
		closeConn(k)
	}
}

/* closeConn remove a conn from the map and closes it, but does not hold the
lock */
func closeConn(c net.Conn) error {
	delete(conns, c)
	return c.Close()
}
