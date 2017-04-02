sshjump
=======
sshjump automates making multiple SSH hops before forwarding ports.

It's intended use-case is for when you've finally gathered a bunch of SSH creds
on an engagement, and the defenders are starting to catch on.

Or, you know, just to annoy the SOC.

Please don't use for illegal purposes.

Example
-------
For the impatient:

Create a jumpfile named j containing hosts through which to jump, similar to
the following:
```
user@target1 password SSH-2.0-OpenSSH_6.7
root@target2 pa$$w0rd SSH-2.0-SOC_wont_find_me
oracle@target3 oracle ssh_2.0-PuTTY
```
Jump through them with the following, pointing local port 2222 at target4's
port 22 (presumably to use SSH's `-D` or something).
```bash
sshjump -jumps ./j -njump 0 L127.0.0.1,2222,target4,22
```

Description
-----------
sshjump makes a series of jumps through SSH servers.  It then forwards ports
through the last server, similar to OpenSSH's `-L` and `-R` options.  This is
useful in obscuring the true origin of a connection during a pentest or red
team engagement.

The jumps are read from a file (the jumpfile), which should contain the
username, hostname, and password for the SSH server, as well as the SSH version
string (e.g. `SSH-2.0-OpenSSH_7.3`) to presesnt to the server.  A subset of the
jumps in the jumpfile may be used (`-njump`, by default the first 5), and the
order in which jumps are tried may be shuffled to further confuse the
defenders (`-shuffle`).

Installation
------------
Standard Go procedure
```bash
go get -u github.com/magisterquis/sshjump
go install github.com/magisterquis/sshjump
```

Usage
-----
```
Usage: sshjump [options] fwdspec [fwdspec...]

The jumpfile must contain lines of the form
user@host password versionstring

Each fwdspec should be of one of the following forms

L<laddr>,<lport>,<targetaddr>,<targetport>
R<raddr>,<rport>,<targetaddr>,<targetport>

The fwdspecs are similar to OpenSSH's -L and -R options, but always consist of
two address/port pairs.

Options:
  -connto timeout
    	TCP connection timeout (default 10s)
  -exittest target
    	Host and port on target to test last jump forwarding ability (default "check.torproject.org:443")
  -hsto timeout
    	SSH handshake timeout (default 15s)
  -jumps file
    	Name of file containing SSH jumps
  -kaint interval
    	SSH keepalive interval (default 1s)
  -njump N
    	The first N working jumps in the jumpfile will be used, or 0 to use all of the jumps (default 5)
  -shuffle
    	Shuffle the list of jumps
```

Use in Production
-----------------
This code hasn't been very thoroughly tested, so please don't use it in
production without sufficient testing, unless you don't particularly like your
client.  Feel free to send feature requests, bug reports, and bugfixes if you
do.

It should compile and run just fine on Windows (I'm looking at you, former
employer), which is handy for those "here, have a user desktop, don't plug in
your computer" situations.
