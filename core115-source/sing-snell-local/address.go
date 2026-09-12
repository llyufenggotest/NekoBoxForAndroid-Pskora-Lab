package snell

import (
	"encoding/binary"
	"io"

	"github.com/sagernet/sing/common/buf"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
)

var udpIPSerializer = M.NewSerializer(
	M.AddressFamilyByte(AddressTypeIPv4, M.AddressFamilyIPv4),
	M.AddressFamilyByte(AddressTypeIPv6, M.AddressFamilyIPv6),
)

var udpResponseSerializer = M.NewSerializer(
	M.AddressFamilyByte(AddressTypeDomain, M.AddressFamilyFqdn),
	M.AddressFamilyByte(AddressTypeIPv4, M.AddressFamilyIPv4),
	M.AddressFamilyByte(AddressTypeIPv6, M.AddressFamilyIPv6),
)

const MaxUDPResponseAddressLen = 1 + 1 + 255 + 2

// Surge 6.7.0 (11520): SNConnectorV4::targetHandshakeData: CONNECT targets are host strings even for literal IPs.
func WriteConnectAddress(buffer *buf.Buffer, destination M.Socksaddr) error {
	host := destination.AddrString()
	if len(host) > 255 {
		return E.New("snell: host too long: ", host)
	}
	err := M.WriteSocksString(buffer, host)
	if err != nil {
		return err
	}
	binary.BigEndian.PutUint16(buffer.Extend(2), destination.Port)
	return nil
}

func ReadConnectAddress(reader io.Reader) (M.Socksaddr, error) {
	host, err := M.ReadSockString(reader)
	if err != nil {
		return M.Socksaddr{}, err
	}
	var portBytes [2]byte
	_, err = io.ReadFull(reader, portBytes[:])
	if err != nil {
		return M.Socksaddr{}, err
	}
	return M.ParseSocksaddrHostPort(host, binary.BigEndian.Uint16(portBytes[:])), nil
}

func WriteUDPRequestAddress(buffer *buf.Buffer, destination M.Socksaddr) error {
	// Surge 6.7.0 (11520): SGUDPConnectorSnellV4::sendData:toHostname:port:metadata: writes 0x00 before literal IP UDP targets.
	if destination.IsIP() {
		err := buffer.WriteByte(0x00)
		if err != nil {
			return err
		}
		return udpIPSerializer.WriteAddrPort(buffer, destination.Unwrap())
	} else {
		host := destination.Fqdn
		if len(host) == 0 || len(host) > 255 {
			return E.New("snell: invalid udp host: ", host)
		}
		err := M.WriteSocksString(buffer, host)
		if err != nil {
			return err
		}
	}
	binary.BigEndian.PutUint16(buffer.Extend(2), destination.Port)
	return nil
}

func ReadUDPRequestAddress(buffer *buf.Buffer) (M.Socksaddr, error) {
	first, err := buffer.ReadByte()
	if err != nil {
		return M.Socksaddr{}, err
	}
	if first != 0x00 {
		host, hostErr := buffer.ReadBytes(int(first))
		if hostErr != nil {
			return M.Socksaddr{}, hostErr
		}
		port, portErr := buffer.ReadBytes(2)
		if portErr != nil {
			return M.Socksaddr{}, portErr
		}
		return M.ParseSocksaddrHostPort(string(host), binary.BigEndian.Uint16(port)), nil
	}
	return udpIPSerializer.ReadAddrPort(buffer)
}

func WriteUDPResponseAddress(buffer *buf.Buffer, source M.Socksaddr) error {
	return writeUDPResponseAddress(buffer, source, false)
}

func WriteUDPResponseAddressPreserveMapped(buffer *buf.Buffer, source M.Socksaddr) error {
	return writeUDPResponseAddress(buffer, source, true)
}

func writeUDPResponseAddress(buffer *buf.Buffer, source M.Socksaddr, preserveMapped bool) error {
	if source.IsFqdn() {
		return udpResponseSerializer.WriteAddrPort(buffer, source)
	}
	if !preserveMapped {
		source = source.Unwrap()
	}
	addr := source.Addr
	if !addr.IsValid() {
		return E.New("snell: udp response source is not an ip: ", source)
	}
	return udpResponseSerializer.WriteAddrPort(buffer, source)
}

func ReadUDPResponseAddress(buffer *buf.Buffer) (M.Socksaddr, error) {
	return udpResponseSerializer.ReadAddrPort(buffer)
}

func UDPResponseAddressLen(source M.Socksaddr) int {
	return udpResponseAddressLen(source, false)
}

func UDPResponseAddressLenPreserveMapped(source M.Socksaddr) int {
	return udpResponseAddressLen(source, true)
}

func udpResponseAddressLen(source M.Socksaddr, preserveMapped bool) int {
	if source.IsFqdn() {
		return 1 + 1 + len(source.Fqdn) + 2
	}
	if !preserveMapped {
		source = source.Unwrap()
	}
	if source.Addr.Is4() {
		return 1 + 4 + 2
	}
	return 1 + 16 + 2
}
