package netease

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// AddComment posts content as the listener's comment on a thread, a
// track's (see Track.Thread), as NetEase's own apps do: it shows there as
// on any other client. It returns the comment as NetEase keeps it.
func AddComment(thread, content string) (*Comment, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil, errors.New("netease: an empty comment")
	}
	var res struct {
		apiError
		Comment Comment `json:"comment"`
	}
	if err := eapiPost("/api/resource/comments/add", map[string]any{"threadId": thread, "content": content}, &res); err != nil {
		return nil, err
	}
	if res.Code != 200 {
		return nil, commentError(res.apiError)
	}
	return &res.Comment, nil
}

// DeleteComment deletes a comment of the listener's on a thread.
func DeleteComment(thread string, id int64) error {
	var res apiError
	if err := eapiPost("/api/resource/comments/delete", map[string]any{"threadId": thread, "commentId": strconv.FormatInt(id, 10)}, &res); err != nil {
		return err
	}
	if res.Code != 200 {
		return commentError(res)
	}
	return nil
}

// A CommentError is NetEase refusing a comment, with what it said, as that
// the listener must bind a phone first.
type CommentError struct {
	Code    int
	Message string
}

func (e *CommentError) Error() string {
	return fmt.Sprintf("netease: comment: code %d %s", e.Code, e.Message)
}

func commentError(res apiError) error {
	msg := res.Message
	if msg == "" {
		msg = res.Msg
	}
	return &CommentError{Code: res.Code, Message: msg}
}
