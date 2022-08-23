package nflog

import (
	"bytes"
	"encoding/binary"
	"log"
	"time"

	"github.com/florianl/go-nflog/v2/internal/unix"

	"github.com/mdlayher/netlink"
)

func extractAttribute(a *Attribute, logger *log.Logger, data []byte) error {
	ad, err := netlink.NewAttributeDecoder(data)
	if err != nil {
		return err
	}
	ad.ByteOrder = nativeEndian
	for ad.Next() {
		switch ad.Type() {
		case nfUlaAttrPacketHdr:
			hwProtocol := binary.BigEndian.Uint16(ad.Bytes()[:2])
			a.HwProtocol = &hwProtocol
			hook := uint8(ad.Bytes()[3])
			a.Hook = &hook
		case nfUlaAttrMark:
			ad.ByteOrder = binary.BigEndian
			mark := ad.Uint32()
			a.Mark = &mark
			ad.ByteOrder = nativeEndian
		case nfUlaAttrTimestamp:
			var sec, usec int64
			r := bytes.NewReader(ad.Bytes()[:8])
			if err := binary.Read(r, binary.BigEndian, &sec); err != nil {
				return err
			}
			r = bytes.NewReader(ad.Bytes()[8:])
			if err := binary.Read(r, binary.BigEndian, &usec); err != nil {
				return err
			}
			timestamp := time.Unix(sec, usec*1000)
			a.Timestamp = &timestamp
		case nfUlaAttrIfindexIndev:
			ad.ByteOrder = binary.BigEndian
			inDev := ad.Uint32()
			a.InDev = &inDev
			ad.ByteOrder = nativeEndian
		case nfUlaAttrIfindexOutdev:
			ad.ByteOrder = binary.BigEndian
			outDev := ad.Uint32()
			a.OutDev = &outDev
			ad.ByteOrder = nativeEndian
		case nfUlaAttrIfindexPhysIndev:
			ad.ByteOrder = binary.BigEndian
			physInDev := ad.Uint32()
			a.PhysInDev = &physInDev
			ad.ByteOrder = nativeEndian
		case nfUlaAttrIfindexPhysOutdev:
			ad.ByteOrder = binary.BigEndian
			physOutDev := ad.Uint32()
			a.PhysOutDev = &physOutDev
			ad.ByteOrder = nativeEndian
		case nfUlaAttrHwaddr:
			hwAddrLen := binary.BigEndian.Uint16(ad.Bytes()[:2])
			hwAddr := (ad.Bytes())[4 : 4+hwAddrLen]
			a.HwAddr = &hwAddr
		case nfUlaAttrPayload:
			payload := ad.Bytes()
			a.Payload = &payload
		case nfUlaAttrPrefix:
			prefix := ad.String()
			a.Prefix = &prefix
		case nfUlaAttrUID:
			ad.ByteOrder = binary.BigEndian
			uid := ad.Uint32()
			a.UID = &uid
			ad.ByteOrder = nativeEndian
		case nfUlaAttrSeq:
			ad.ByteOrder = binary.BigEndian
			seq := ad.Uint32()
			a.Seq = &seq
			ad.ByteOrder = nativeEndian
		case nfUlaAttrSeqGlobal:
			ad.ByteOrder = binary.BigEndian
			seqGlobal := ad.Uint32()
			a.SeqGlobal = &seqGlobal
			ad.ByteOrder = nativeEndian
		case nfUlaAttrGID:
			ad.ByteOrder = binary.BigEndian
			gid := ad.Uint32()
			a.GID = &gid
			ad.ByteOrder = nativeEndian
		case nfUlaAttrHwType:
			ad.ByteOrder = binary.BigEndian
			hwType := ad.Uint16()
			a.HwType = &hwType
			ad.ByteOrder = nativeEndian
		case nfUlaAttrHwHeader:
			hwHeader := ad.Bytes()
			a.HwHeader = &hwHeader
		case nfUlaAttrHwLen:
			ad.ByteOrder = binary.BigEndian
			hwLen := ad.Uint16()
			a.HwLen = &hwLen
			ad.ByteOrder = nativeEndian
		case nfUlaAttrCt:
			ad.ByteOrder = binary.BigEndian
			ct := ad.Bytes()
			a.Ct = &ct
			ad.ByteOrder = nativeEndian
		case nfUlaAttrCtInfo:
			ad.ByteOrder = binary.BigEndian
			ctInfo := ad.Uint32()
			a.CtInfo = &ctInfo
			ad.ByteOrder = nativeEndian
		default:
			logger.Printf("Unknown attribute: %d %v\n", ad.Type(), ad.Bytes())
		}
	}

	return ad.Err()
}

func checkHeader(data []byte) int {
	if (data[0] == unix.AF_INET || data[0] == unix.AF_INET6) && data[1] == unix.NFNETLINK_V0 {
		return 4
	}
	return 0
}

func extractAttributes(logger *log.Logger, msg []byte) (Attribute, error) {
	attrs := Attribute{}

	offset := checkHeader(msg[:2])
	if err := extractAttribute(&attrs, logger, msg[offset:]); err != nil {
		return attrs, err
	}
	return attrs, nil
}
