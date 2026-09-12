package vless

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"io"

	"github.com/sagernet/sing-vmess"
	"github.com/sagernet/sing/common"
	"github.com/sagernet/sing/common/buf"
	E "github.com/sagernet/sing/common/exceptions"
	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/common/rw"
	"github.com/sagernet/sing/common/varbin"
)

const (
	Version          = 0
	FlowVision       = "xtls-rprx-vision"
	juziSDKTagLength = 8
)

var juziSDKTagKey = []byte("hello_pidun")

func buildJuziSDKTag(version byte, userID [16]byte) [juziSDKTagLength]byte {
	mac := hmac.New(sha256.New, juziSDKTagKey)
	_, _ = mac.Write([]byte{version})
	_, _ = mac.Write(userID[:])
	var tag [juziSDKTagLength]byte
	copy(tag[:], mac.Sum(nil))
	return tag
}

type Request struct {
	UUID        [16]byte
	Command     byte
	Destination M.Socksaddr
	Flow        string
	IsX365      bool // X365 private framing
	IsJuzi      bool // insert Juzi SDK tag after UUID
}

func ReadRequest(reader io.Reader) (*Request, error) {
	var request Request

	var version uint8
	err := binary.Read(reader, binary.BigEndian, &version)
	if err != nil {
		return nil, err
	}
	if version != Version {
		return nil, E.New("unknown version: ", version)
	}

	_, err = io.ReadFull(reader, request.UUID[:])
	if err != nil {
		return nil, err
	}

	var addonsLen uint8
	err = binary.Read(reader, binary.BigEndian, &addonsLen)
	if err != nil {
		return nil, err
	}

	if addonsLen > 0 {
		addonsBytes := make([]byte, addonsLen)
		_, err = io.ReadFull(reader, addonsBytes)
		if err != nil {
			return nil, err
		}

		addons, err := readAddons(bytes.NewReader(addonsBytes))
		if err != nil {
			return nil, err
		}
		request.Flow = addons.Flow
	}

	err = binary.Read(reader, binary.BigEndian, &request.Command)
	if err != nil {
		return nil, err
	}

	if request.Command != vmess.CommandMux {
		request.Destination, err = vmess.AddressSerializer.ReadAddrPort(reader)
		if err != nil {
			return nil, err
		}
	}

	return &request, nil
}

type Addons struct {
	Flow string
	Seed string
}

func readAddons(reader *bytes.Reader) (*Addons, error) {
	var addons Addons
	for reader.Len() > 0 {
		protoHeader, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		switch protoHeader {
		case (1 << 3) | 2:
			flowLen, err := binary.ReadUvarint(reader)
			if err != nil {
				return nil, err
			}
			flowBytes := make([]byte, flowLen)
			_, err = io.ReadFull(reader, flowBytes)
			if err != nil {
				return nil, err
			}
			addons.Flow = string(flowBytes)
		case (2 << 3) | 2:
			seedLen, err := binary.ReadUvarint(reader)
			if err != nil {
				return nil, err
			}
			seedBytes := make([]byte, seedLen)
			_, err = io.ReadFull(reader, seedBytes)
			if err != nil {
				return nil, err
			}
			addons.Seed = string(seedBytes)
		default:
			return nil, E.New("unknown protobuf message header: ", protoHeader)
		}
	}
	return &addons, nil
}

func RequestLen(request Request) int {
	var requestLen int

	if request.IsX365 {
		// ✨ X365 的长度计算
		requestLen += 5  // X365\x01
		requestLen += 1  // command
		requestLen += 16 // uuid
		if request.Command != vmess.CommandMux {
			requestLen += 2 // port
			requestLen += 1 // atyp
			if request.Destination.IsFqdn() {
				requestLen += 1 // domain length byte
				requestLen += len(request.Destination.Fqdn)
			} else if request.Destination.IsIPv4() {
				requestLen += 4
			} else {
				requestLen += 16
			}
		}
		return requestLen
	}

	// Original VLESS length plus the Juzi tag when explicitly enabled.
	requestLen += 1  // version
	requestLen += 16 // uuid
	if request.IsJuzi {
		requestLen += juziSDKTagLength
	}
	requestLen += 1 // protobuf length

	var addonsLen int
	if request.Flow != "" {
		addonsLen += 1 // protobuf header
		addonsLen += varbin.UvarintLen(uint64(len(request.Flow)))
		addonsLen += len(request.Flow)
		requestLen += addonsLen
	}
	requestLen += 1 // command
	if request.Command != vmess.CommandMux {
		requestLen += vmess.AddressSerializer.AddrPortLen(request.Destination)
	}
	return requestLen
}

func EncodeRequest(request Request, buffer *buf.Buffer) error {
	if request.IsX365 {
		// ==========================================
		// ✨ X365 魔改协议拼包逻辑
		// ==========================================
		buffer.Write([]byte{'X', '3', '6', '5', 0x01})
		buffer.WriteByte(request.Command)
		buffer.Write(request.UUID[:])

		if request.Command != vmess.CommandMux {
			binary.BigEndian.PutUint16(buffer.Extend(2), request.Destination.Port)

			if request.Destination.IsFqdn() {
				buffer.WriteByte(2)
				buffer.WriteByte(byte(len(request.Destination.Fqdn)))
				buffer.WriteString(request.Destination.Fqdn)
			} else if request.Destination.IsIPv4() {
				buffer.WriteByte(1)
				buffer.Write(request.Destination.Addr.AsSlice())
			} else {
				buffer.WriteByte(3)
				buffer.Write(request.Destination.Addr.AsSlice())
			}
		}
		return nil
	}

	// ==========================================
	// 原版 VLESS 拼包逻辑
	// ==========================================
	var addonsLen int
	if request.Flow != "" {
		addonsLen += 1 // protobuf header
		addonsLen += varbin.UvarintLen(uint64(len(request.Flow)))
		addonsLen += len(request.Flow)
	}

	common.Must(
		buffer.WriteByte(Version),
		common.Error(buffer.Write(request.UUID[:])),
	)
	if request.IsJuzi {
		tag := buildJuziSDKTag(Version, request.UUID)
		common.Must(common.Error(buffer.Write(tag[:])))
	}
	common.Must(buffer.WriteByte(byte(addonsLen)))
	if addonsLen > 0 {
		common.Must(buffer.WriteByte(10))
		binary.PutUvarint(buffer.Extend(varbin.UvarintLen(uint64(len(request.Flow)))), uint64(len(request.Flow)))
		common.Must(common.Error(buffer.WriteString(request.Flow)))
	}
	common.Must(
		buffer.WriteByte(request.Command),
	)

	if request.Command != vmess.CommandMux {
		err := vmess.AddressSerializer.WriteAddrPort(buffer, request.Destination)
		if err != nil {
			return err
		}
	}
	return nil
}

func WriteRequest(writer io.Writer, request Request, payload []byte) error {
	requestLen := RequestLen(request)
	requestLen += len(payload)
	buffer := buf.NewSize(requestLen)
	defer buffer.Release()

	err := EncodeRequest(request, buffer)
	if err != nil {
		return err
	}

	if len(payload) > 0 {
		common.Must1(buffer.Write(payload))
	}
	return common.Error(writer.Write(buffer.Bytes()))
}

func WritePacketRequest(writer io.Writer, request Request, payload []byte) error {
	requestLen := RequestLen(request)
	if len(payload) > 0 {
		requestLen += 2
		requestLen += len(payload)
	}
	buffer := buf.NewSize(requestLen)
	defer buffer.Release()

	err := EncodeRequest(request, buffer)
	if err != nil {
		return err
	}

	if len(payload) > 0 {
		common.Must(
			binary.Write(buffer, binary.BigEndian, uint16(len(payload))),
			common.Error(buffer.Write(payload)),
		)
	}

	return common.Error(writer.Write(buffer.Bytes()))
}

// ✨ 完美宽容校验：同时放行 X365暗号 和 原版 VLESS (0)
func ReadResponse(reader io.Reader, isX365 bool) error {
	if isX365 {
		// X365 接收 5 字节暗号并验证
		header := make([]byte, 5)
		_, err := io.ReadFull(reader, header)
		if err != nil {
			return err
		}
		if header[0] != 'X' || header[1] != '3' || header[2] != '6' || header[3] != '5' {
			return E.New("invalid x365 response header")
		}
		return nil
	}

	var version byte
	err := binary.Read(reader, binary.BigEndian, &version)
	if err != nil {
		return err
	}
	if version != Version {
		return E.New("unknown version: ", version)
	}
	var protobufLength byte
	err = binary.Read(reader, binary.BigEndian, &protobufLength)
	if err != nil {
		return err
	}
	if protobufLength > 0 {
		err = rw.SkipN(reader, int(protobufLength))
		if err != nil {
			return err
		}
	}
	return nil
}
