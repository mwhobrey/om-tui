package cookiebridge

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
)

// MaxNativeMessageBytes is Chrome's native messaging payload limit.
const MaxNativeMessageBytes = 1024 * 1024

// ReadNativeMessage reads one length-prefixed JSON message from r.
func ReadNativeMessage(r io.Reader, dst any) error {
	var length uint32
	if err := binary.Read(r, binary.LittleEndian, &length); err != nil {
		return err
	}
	if length == 0 || length > MaxNativeMessageBytes {
		return fmt.Errorf("invalid native message length %d", length)
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return err
	}
	return json.Unmarshal(buf, dst)
}

// WriteNativeMessage writes one length-prefixed JSON message to w.
func WriteNativeMessage(w io.Writer, msg any) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if len(payload) > MaxNativeMessageBytes {
		return fmt.Errorf("native message too large (%d bytes)", len(payload))
	}
	if err := binary.Write(w, binary.LittleEndian, uint32(len(payload))); err != nil {
		return err
	}
	_, err = w.Write(payload)
	return err
}
