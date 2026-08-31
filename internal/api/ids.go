package api

import (
	"encoding/base64"
	"errors"
	"strconv"
)

var errBadID = errors.New("invalid id")

func encID(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(id, 10)))
}

func decID(s string) (int64, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return 0, errBadID
	}
	id, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return 0, errBadID
	}
	return id, nil
}
