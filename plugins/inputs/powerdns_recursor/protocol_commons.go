package powerdns_recursor

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
	"unsafe"
)

func parseResponse(metrics string) map[string]interface{} {
	values := make(map[string]interface{})

	s := strings.Split(metrics, "\n")

	for _, metric := range s[:len(s)-1] {
		m := strings.Split(metric, "\t")
		if len(m) < 2 {
			continue
		}

		i, err := strconv.ParseInt(m[1], 10, 64)
		if err != nil {
			continue
		}

		values[m[0]] = i
	}

	return values
}

// This below is generally unsafe but necessary in this case
// since the powerdns protocol encoding is host dependent.
// The C implementation uses size_t as the size type for the
// command length. The size and endianness of size_t change
// depending on the platform the program is being run on.
// At the time of writing, the Go type `uint` has the same
// behavior, where its size and endianness are platform
// dependent. Using the unsafe method below, and the known
// integer size, we can "recreate" the corresponding C
// behavior in an effort to maintain compatibility. Of course
// in cases where one program is compiled for i386 and the
// other for amd64 (and similar), this method will fail.

const uintSizeInBytes = strconv.IntSize / 8

func getEndianness() binary.ByteOrder {
	buf := make([]byte, 2)
	*(*uint16)(unsafe.Pointer(&buf[0])) = uint16(0x0001)

	if buf[0] == 1 {
		return binary.LittleEndian
	}

	return binary.BigEndian
}

func writeNativeUIntToConn(conn net.Conn, value uint) (int, error) {
	intData := make([]byte, uintSizeInBytes)

	if uintSizeInBytes == 4 {
		getEndianness().PutUint32(intData, uint32(value))
		return conn.Write(intData)
	} else if uintSizeInBytes == 8 {
		getEndianness().PutUint64(intData, uint64(value))
		return conn.Write(intData)
	}

	return 0, fmt.Errorf("unsupported system configuration")
}

func readNativeUIntFromConn(conn net.Conn) (uint, error) {
	intData := make([]byte, uintSizeInBytes)

	n, err := conn.Read(intData)

	if err != nil {
		return 0, err
	}

	if n != uintSizeInBytes {
		return 0, fmt.Errorf("did not read enough data for native uint: read '%v' bytes, expected '%v'", n, uintSizeInBytes)
	}

	if uintSizeInBytes == 4 {
		return uint(getEndianness().Uint32(intData)), nil
	} else if uintSizeInBytes == 8 {
		return uint(getEndianness().Uint64(intData)), nil
	} else {
		return 0, fmt.Errorf("unsupported system configuration")
	}
}
