package minecraft

import (
	"ZBProxy/config"
	"fmt"
	mcnet "github.com/Tnze/go-mc/net"
	"github.com/Tnze/go-mc/net/packet"
	"github.com/fatih/color"
	"log"
	"net"
)

func badPacketPanicRecover(s *config.ConfigProxyService) {
	// Non-Minecraft packet which uses `go-mc` packet scan method may cause panic.
	// So a panic handler is needed.
	if err := recover(); err != nil {
		log.Printf(color.HiRedString("Service %s : Bad Minecraft packet was received: %v", s.Name, err))
	}
}

func NewConnHandler(s *config.ConfigProxyService, c *net.Conn) (*mcnet.Conn, error) {
	defer badPacketPanicRecover(s)

	conn := mcnet.WrapConn(*c)
	var p packet.Packet
	err := conn.ReadPacket(&p)
	if err != nil {
		return nil, err
	}

	var ( // Server bound : Handshake
		protocol  packet.VarInt
		hostname  packet.String
		port      packet.UnsignedShort
		nextState packet.VarInt
	)
	err = p.Scan(&protocol, &hostname, &port, &nextState)
	if err != nil {
		return nil, err
	}
	if nextState == 1 { // status
		if s.MotdDescription == "" && s.MotdFavicon == "" {
			// directly proxy MOTD from server
			remote, err := mcnet.DialMC(fmt.Sprintf("%v:%v", s.TargetAddress, s.TargetPort))
			if err != nil {
				return nil, err
			}

			remote.WritePacket(p)      // Server bound : Handshake
			remote.Write([]byte{1, 0}) // Server bound : Status Request
			return remote, nil
		} else {
			// Server bound : Status Request
			// Must read, but not used (and also nothing included in it)
			conn.ReadPacket(&p)

			// send custom MOTD
			conn.WritePacket(generateMotdPacket(
				int(protocol),
				s.MotdFavicon, s.MotdDescription))

			// handle for ping request
			conn.ReadPacket(&p)
			conn.WritePacket(p)

			conn.Close()
			return conn, nil
		}
	}
	// else: login

	// Server bound : Login Start
	// Get player name and check the profile
	conn.ReadPacket(&p)
	var (
		playerName packet.String
	)
	err = p.Scan(&playerName)
	if err != nil {
		return nil, err
	}
	log.Printf("Service %s : A new Minecraft player requested a login: %s", s.Name, playerName)
	// TODO PlayerName handle

	remote, err := mcnet.DialMC(fmt.Sprintf("%v:%v", s.TargetAddress, s.TargetPort))
	if err != nil {
		log.Printf("Service %s : Failed to dial to target server: %v", s.Name, err.Error())
		conn.Close()
		return nil, err
	}

	// Hostname rewritten
	if s.EnableHostnameRewrite {
		err = remote.WritePacket(packet.Marshal(
			0x0, // Server bound : Handshake
			protocol,
			packet.String(s.RewrittenHostname),
			packet.UnsignedShort(s.TargetPort),
			packet.Byte(2),
		))
	} else {
		err = remote.WritePacket(packet.Marshal(
			0x0, // Server bound : Handshake
			protocol,
			hostname,
			port,
			packet.Byte(2),
		))
	}
	if err != nil {
		return nil, err
	}

	// Server bound : Login Start
	err = remote.WritePacket(p)
	if err != nil {
		return nil, err
	}
	return remote, nil
}
