package link

import (
	"fmt"

	"google.golang.org/protobuf/proto"
)

// unmarshalProto decodes protobuf bytes into msg.
func unmarshalProto(b []byte, msg proto.Message) error {
	if err := proto.Unmarshal(b, msg); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}
	return nil
}

// marshalProto encodes msg to protobuf bytes.
func marshalProto(msg proto.Message) ([]byte, error) {
	b, err := proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	return b, nil
}
