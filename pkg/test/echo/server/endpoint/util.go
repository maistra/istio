// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package endpoint

import (
	"crypto/tls"
	"net"
	"os"
	"strconv"
	"time"

	"istio.io/istio/pkg/log"
)

var epLog = log.RegisterScope("endpoint", "echo serverside")

const (
	requestTimeout = 15 * time.Second
	idleTimeout    = 5 * time.Second
)

func listenOnAddress(ip string, port int) (net.Listener, int, error) {
	parsedIP := net.ParseIP(ip)
	ipBind := "tcp"
	if parsedIP != nil {
		if parsedIP.To4() == nil && parsedIP.To16() != nil {
			ipBind = "tcp6"
		} else if parsedIP.To4() != nil {
			ipBind = "tcp4"
		}
	}
	ln, err := net.Listen(ipBind, net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return nil, 0, err
	}

	port = ln.Addr().(*net.TCPAddr).Port
	return ln, port, nil
}

func listenOnAddressTLS(ip string, port int, cfg *tls.Config) (net.Listener, int, error) {
	ipBind := "tcp"
	parsedIP := net.ParseIP(ip)
	if parsedIP != nil {
		if parsedIP.To4() == nil && parsedIP.To16() != nil {
			ipBind = "tcp6"
		} else if parsedIP.To4() != nil {
			ipBind = "tcp4"
		}
	}
	ln, err := tls.Listen(ipBind, net.JoinHostPort(ip, strconv.Itoa(port)), cfg)
	if err != nil {
		return nil, 0, err
	}
	port = ln.Addr().(*net.TCPAddr).Port
	return ln, port, nil
}

func listenOnUDS(uds string) (net.Listener, error) {
	_ = os.Remove(uds)
	ln, err := net.Listen("unix", uds)
	if err != nil {
		return nil, err
	}

	return ln, nil
}

func listenUDPAddress(ip string, port int) (net.PacketConn, int, error) {
	parsedIP := net.ParseIP(ip)
	ipBind := "udp"
	if parsedIP != nil {
		if parsedIP.To4() == nil && parsedIP.To16() != nil {
			ipBind = "udp6"
		} else if parsedIP.To4() != nil {
			ipBind = "udp4"
		}
	}
	ln, err := net.ListenPacket(ipBind, net.JoinHostPort(ip, strconv.Itoa(port)))
	if err != nil {
		return nil, 0, err
	}

	port = ln.LocalAddr().(*net.UDPAddr).Port
	return ln, port, nil
}

// forceClose the given socket.
func forceClose(conn net.Conn) error {
	// Close may be called more than once.
	defer func() { _ = conn.Close() }()

	// Force the connection closed (should result in sending RST)
	return conn.(*net.TCPConn).SetLinger(0)
}
