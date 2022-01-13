// Code generated by protoc-gen-validate. DO NOT EDIT.
// source: envoy/service/accesslog/v2/als.proto

package envoy_service_accesslog_v2

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"google.golang.org/protobuf/types/known/anypb"
)

// ensure the imports are used
var (
	_ = bytes.MinRead
	_ = errors.New("")
	_ = fmt.Print
	_ = utf8.UTFMax
	_ = (*regexp.Regexp)(nil)
	_ = (*strings.Reader)(nil)
	_ = net.IPv4len
	_ = time.Duration(0)
	_ = (*url.URL)(nil)
	_ = (*mail.Address)(nil)
	_ = anypb.Any{}
	_ = sort.Sort
)

// Validate checks the field values on StreamAccessLogsResponse with the rules
// defined in the proto definition for this message. If any rules are
// violated, the first error encountered is returned, or nil if there are no violations.
func (m *StreamAccessLogsResponse) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on StreamAccessLogsResponse with the
// rules defined in the proto definition for this message. If any rules are
// violated, the result is a list of violation errors wrapped in
// StreamAccessLogsResponseMultiError, or nil if none found.
func (m *StreamAccessLogsResponse) ValidateAll() error {
	return m.validate(true)
}

func (m *StreamAccessLogsResponse) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	if len(errors) > 0 {
		return StreamAccessLogsResponseMultiError(errors)
	}
	return nil
}

// StreamAccessLogsResponseMultiError is an error wrapping multiple validation
// errors returned by StreamAccessLogsResponse.ValidateAll() if the designated
// constraints aren't met.
type StreamAccessLogsResponseMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m StreamAccessLogsResponseMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m StreamAccessLogsResponseMultiError) AllErrors() []error { return m }

// StreamAccessLogsResponseValidationError is the validation error returned by
// StreamAccessLogsResponse.Validate if the designated constraints aren't met.
type StreamAccessLogsResponseValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e StreamAccessLogsResponseValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e StreamAccessLogsResponseValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e StreamAccessLogsResponseValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e StreamAccessLogsResponseValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e StreamAccessLogsResponseValidationError) ErrorName() string {
	return "StreamAccessLogsResponseValidationError"
}

// Error satisfies the builtin error interface
func (e StreamAccessLogsResponseValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sStreamAccessLogsResponse.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = StreamAccessLogsResponseValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = StreamAccessLogsResponseValidationError{}

// Validate checks the field values on StreamAccessLogsMessage with the rules
// defined in the proto definition for this message. If any rules are
// violated, the first error encountered is returned, or nil if there are no violations.
func (m *StreamAccessLogsMessage) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on StreamAccessLogsMessage with the
// rules defined in the proto definition for this message. If any rules are
// violated, the result is a list of violation errors wrapped in
// StreamAccessLogsMessageMultiError, or nil if none found.
func (m *StreamAccessLogsMessage) ValidateAll() error {
	return m.validate(true)
}

func (m *StreamAccessLogsMessage) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	if all {
		switch v := interface{}(m.GetIdentifier()).(type) {
		case interface{ ValidateAll() error }:
			if err := v.ValidateAll(); err != nil {
				errors = append(errors, StreamAccessLogsMessageValidationError{
					field:  "Identifier",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		case interface{ Validate() error }:
			if err := v.Validate(); err != nil {
				errors = append(errors, StreamAccessLogsMessageValidationError{
					field:  "Identifier",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		}
	} else if v, ok := interface{}(m.GetIdentifier()).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return StreamAccessLogsMessageValidationError{
				field:  "Identifier",
				reason: "embedded message failed validation",
				cause:  err,
			}
		}
	}

	switch m.LogEntries.(type) {

	case *StreamAccessLogsMessage_HttpLogs:

		if all {
			switch v := interface{}(m.GetHttpLogs()).(type) {
			case interface{ ValidateAll() error }:
				if err := v.ValidateAll(); err != nil {
					errors = append(errors, StreamAccessLogsMessageValidationError{
						field:  "HttpLogs",
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			case interface{ Validate() error }:
				if err := v.Validate(); err != nil {
					errors = append(errors, StreamAccessLogsMessageValidationError{
						field:  "HttpLogs",
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			}
		} else if v, ok := interface{}(m.GetHttpLogs()).(interface{ Validate() error }); ok {
			if err := v.Validate(); err != nil {
				return StreamAccessLogsMessageValidationError{
					field:  "HttpLogs",
					reason: "embedded message failed validation",
					cause:  err,
				}
			}
		}

	case *StreamAccessLogsMessage_TcpLogs:

		if all {
			switch v := interface{}(m.GetTcpLogs()).(type) {
			case interface{ ValidateAll() error }:
				if err := v.ValidateAll(); err != nil {
					errors = append(errors, StreamAccessLogsMessageValidationError{
						field:  "TcpLogs",
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			case interface{ Validate() error }:
				if err := v.Validate(); err != nil {
					errors = append(errors, StreamAccessLogsMessageValidationError{
						field:  "TcpLogs",
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			}
		} else if v, ok := interface{}(m.GetTcpLogs()).(interface{ Validate() error }); ok {
			if err := v.Validate(); err != nil {
				return StreamAccessLogsMessageValidationError{
					field:  "TcpLogs",
					reason: "embedded message failed validation",
					cause:  err,
				}
			}
		}

	default:
		err := StreamAccessLogsMessageValidationError{
			field:  "LogEntries",
			reason: "value is required",
		}
		if !all {
			return err
		}
		errors = append(errors, err)

	}

	if len(errors) > 0 {
		return StreamAccessLogsMessageMultiError(errors)
	}
	return nil
}

// StreamAccessLogsMessageMultiError is an error wrapping multiple validation
// errors returned by StreamAccessLogsMessage.ValidateAll() if the designated
// constraints aren't met.
type StreamAccessLogsMessageMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m StreamAccessLogsMessageMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m StreamAccessLogsMessageMultiError) AllErrors() []error { return m }

// StreamAccessLogsMessageValidationError is the validation error returned by
// StreamAccessLogsMessage.Validate if the designated constraints aren't met.
type StreamAccessLogsMessageValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e StreamAccessLogsMessageValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e StreamAccessLogsMessageValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e StreamAccessLogsMessageValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e StreamAccessLogsMessageValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e StreamAccessLogsMessageValidationError) ErrorName() string {
	return "StreamAccessLogsMessageValidationError"
}

// Error satisfies the builtin error interface
func (e StreamAccessLogsMessageValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sStreamAccessLogsMessage.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = StreamAccessLogsMessageValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = StreamAccessLogsMessageValidationError{}

// Validate checks the field values on StreamAccessLogsMessage_Identifier with
// the rules defined in the proto definition for this message. If any rules
// are violated, the first error encountered is returned, or nil if there are
// no violations.
func (m *StreamAccessLogsMessage_Identifier) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on StreamAccessLogsMessage_Identifier
// with the rules defined in the proto definition for this message. If any
// rules are violated, the result is a list of violation errors wrapped in
// StreamAccessLogsMessage_IdentifierMultiError, or nil if none found.
func (m *StreamAccessLogsMessage_Identifier) ValidateAll() error {
	return m.validate(true)
}

func (m *StreamAccessLogsMessage_Identifier) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	if m.GetNode() == nil {
		err := StreamAccessLogsMessage_IdentifierValidationError{
			field:  "Node",
			reason: "value is required",
		}
		if !all {
			return err
		}
		errors = append(errors, err)
	}

	if all {
		switch v := interface{}(m.GetNode()).(type) {
		case interface{ ValidateAll() error }:
			if err := v.ValidateAll(); err != nil {
				errors = append(errors, StreamAccessLogsMessage_IdentifierValidationError{
					field:  "Node",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		case interface{ Validate() error }:
			if err := v.Validate(); err != nil {
				errors = append(errors, StreamAccessLogsMessage_IdentifierValidationError{
					field:  "Node",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		}
	} else if v, ok := interface{}(m.GetNode()).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return StreamAccessLogsMessage_IdentifierValidationError{
				field:  "Node",
				reason: "embedded message failed validation",
				cause:  err,
			}
		}
	}

	if len(m.GetLogName()) < 1 {
		err := StreamAccessLogsMessage_IdentifierValidationError{
			field:  "LogName",
			reason: "value length must be at least 1 bytes",
		}
		if !all {
			return err
		}
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return StreamAccessLogsMessage_IdentifierMultiError(errors)
	}
	return nil
}

// StreamAccessLogsMessage_IdentifierMultiError is an error wrapping multiple
// validation errors returned by
// StreamAccessLogsMessage_Identifier.ValidateAll() if the designated
// constraints aren't met.
type StreamAccessLogsMessage_IdentifierMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m StreamAccessLogsMessage_IdentifierMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m StreamAccessLogsMessage_IdentifierMultiError) AllErrors() []error { return m }

// StreamAccessLogsMessage_IdentifierValidationError is the validation error
// returned by StreamAccessLogsMessage_Identifier.Validate if the designated
// constraints aren't met.
type StreamAccessLogsMessage_IdentifierValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e StreamAccessLogsMessage_IdentifierValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e StreamAccessLogsMessage_IdentifierValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e StreamAccessLogsMessage_IdentifierValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e StreamAccessLogsMessage_IdentifierValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e StreamAccessLogsMessage_IdentifierValidationError) ErrorName() string {
	return "StreamAccessLogsMessage_IdentifierValidationError"
}

// Error satisfies the builtin error interface
func (e StreamAccessLogsMessage_IdentifierValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sStreamAccessLogsMessage_Identifier.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = StreamAccessLogsMessage_IdentifierValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = StreamAccessLogsMessage_IdentifierValidationError{}

// Validate checks the field values on
// StreamAccessLogsMessage_HTTPAccessLogEntries with the rules defined in the
// proto definition for this message. If any rules are violated, the first
// error encountered is returned, or nil if there are no violations.
func (m *StreamAccessLogsMessage_HTTPAccessLogEntries) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on
// StreamAccessLogsMessage_HTTPAccessLogEntries with the rules defined in the
// proto definition for this message. If any rules are violated, the result is
// a list of violation errors wrapped in
// StreamAccessLogsMessage_HTTPAccessLogEntriesMultiError, or nil if none found.
func (m *StreamAccessLogsMessage_HTTPAccessLogEntries) ValidateAll() error {
	return m.validate(true)
}

func (m *StreamAccessLogsMessage_HTTPAccessLogEntries) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	if len(m.GetLogEntry()) < 1 {
		err := StreamAccessLogsMessage_HTTPAccessLogEntriesValidationError{
			field:  "LogEntry",
			reason: "value must contain at least 1 item(s)",
		}
		if !all {
			return err
		}
		errors = append(errors, err)
	}

	for idx, item := range m.GetLogEntry() {
		_, _ = idx, item

		if all {
			switch v := interface{}(item).(type) {
			case interface{ ValidateAll() error }:
				if err := v.ValidateAll(); err != nil {
					errors = append(errors, StreamAccessLogsMessage_HTTPAccessLogEntriesValidationError{
						field:  fmt.Sprintf("LogEntry[%v]", idx),
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			case interface{ Validate() error }:
				if err := v.Validate(); err != nil {
					errors = append(errors, StreamAccessLogsMessage_HTTPAccessLogEntriesValidationError{
						field:  fmt.Sprintf("LogEntry[%v]", idx),
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			}
		} else if v, ok := interface{}(item).(interface{ Validate() error }); ok {
			if err := v.Validate(); err != nil {
				return StreamAccessLogsMessage_HTTPAccessLogEntriesValidationError{
					field:  fmt.Sprintf("LogEntry[%v]", idx),
					reason: "embedded message failed validation",
					cause:  err,
				}
			}
		}

	}

	if len(errors) > 0 {
		return StreamAccessLogsMessage_HTTPAccessLogEntriesMultiError(errors)
	}
	return nil
}

// StreamAccessLogsMessage_HTTPAccessLogEntriesMultiError is an error wrapping
// multiple validation errors returned by
// StreamAccessLogsMessage_HTTPAccessLogEntries.ValidateAll() if the
// designated constraints aren't met.
type StreamAccessLogsMessage_HTTPAccessLogEntriesMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m StreamAccessLogsMessage_HTTPAccessLogEntriesMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m StreamAccessLogsMessage_HTTPAccessLogEntriesMultiError) AllErrors() []error { return m }

// StreamAccessLogsMessage_HTTPAccessLogEntriesValidationError is the
// validation error returned by
// StreamAccessLogsMessage_HTTPAccessLogEntries.Validate if the designated
// constraints aren't met.
type StreamAccessLogsMessage_HTTPAccessLogEntriesValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e StreamAccessLogsMessage_HTTPAccessLogEntriesValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e StreamAccessLogsMessage_HTTPAccessLogEntriesValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e StreamAccessLogsMessage_HTTPAccessLogEntriesValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e StreamAccessLogsMessage_HTTPAccessLogEntriesValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e StreamAccessLogsMessage_HTTPAccessLogEntriesValidationError) ErrorName() string {
	return "StreamAccessLogsMessage_HTTPAccessLogEntriesValidationError"
}

// Error satisfies the builtin error interface
func (e StreamAccessLogsMessage_HTTPAccessLogEntriesValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sStreamAccessLogsMessage_HTTPAccessLogEntries.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = StreamAccessLogsMessage_HTTPAccessLogEntriesValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = StreamAccessLogsMessage_HTTPAccessLogEntriesValidationError{}

// Validate checks the field values on
// StreamAccessLogsMessage_TCPAccessLogEntries with the rules defined in the
// proto definition for this message. If any rules are violated, the first
// error encountered is returned, or nil if there are no violations.
func (m *StreamAccessLogsMessage_TCPAccessLogEntries) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on
// StreamAccessLogsMessage_TCPAccessLogEntries with the rules defined in the
// proto definition for this message. If any rules are violated, the result is
// a list of violation errors wrapped in
// StreamAccessLogsMessage_TCPAccessLogEntriesMultiError, or nil if none found.
func (m *StreamAccessLogsMessage_TCPAccessLogEntries) ValidateAll() error {
	return m.validate(true)
}

func (m *StreamAccessLogsMessage_TCPAccessLogEntries) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	if len(m.GetLogEntry()) < 1 {
		err := StreamAccessLogsMessage_TCPAccessLogEntriesValidationError{
			field:  "LogEntry",
			reason: "value must contain at least 1 item(s)",
		}
		if !all {
			return err
		}
		errors = append(errors, err)
	}

	for idx, item := range m.GetLogEntry() {
		_, _ = idx, item

		if all {
			switch v := interface{}(item).(type) {
			case interface{ ValidateAll() error }:
				if err := v.ValidateAll(); err != nil {
					errors = append(errors, StreamAccessLogsMessage_TCPAccessLogEntriesValidationError{
						field:  fmt.Sprintf("LogEntry[%v]", idx),
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			case interface{ Validate() error }:
				if err := v.Validate(); err != nil {
					errors = append(errors, StreamAccessLogsMessage_TCPAccessLogEntriesValidationError{
						field:  fmt.Sprintf("LogEntry[%v]", idx),
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			}
		} else if v, ok := interface{}(item).(interface{ Validate() error }); ok {
			if err := v.Validate(); err != nil {
				return StreamAccessLogsMessage_TCPAccessLogEntriesValidationError{
					field:  fmt.Sprintf("LogEntry[%v]", idx),
					reason: "embedded message failed validation",
					cause:  err,
				}
			}
		}

	}

	if len(errors) > 0 {
		return StreamAccessLogsMessage_TCPAccessLogEntriesMultiError(errors)
	}
	return nil
}

// StreamAccessLogsMessage_TCPAccessLogEntriesMultiError is an error wrapping
// multiple validation errors returned by
// StreamAccessLogsMessage_TCPAccessLogEntries.ValidateAll() if the designated
// constraints aren't met.
type StreamAccessLogsMessage_TCPAccessLogEntriesMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m StreamAccessLogsMessage_TCPAccessLogEntriesMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m StreamAccessLogsMessage_TCPAccessLogEntriesMultiError) AllErrors() []error { return m }

// StreamAccessLogsMessage_TCPAccessLogEntriesValidationError is the validation
// error returned by StreamAccessLogsMessage_TCPAccessLogEntries.Validate if
// the designated constraints aren't met.
type StreamAccessLogsMessage_TCPAccessLogEntriesValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e StreamAccessLogsMessage_TCPAccessLogEntriesValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e StreamAccessLogsMessage_TCPAccessLogEntriesValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e StreamAccessLogsMessage_TCPAccessLogEntriesValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e StreamAccessLogsMessage_TCPAccessLogEntriesValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e StreamAccessLogsMessage_TCPAccessLogEntriesValidationError) ErrorName() string {
	return "StreamAccessLogsMessage_TCPAccessLogEntriesValidationError"
}

// Error satisfies the builtin error interface
func (e StreamAccessLogsMessage_TCPAccessLogEntriesValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sStreamAccessLogsMessage_TCPAccessLogEntries.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = StreamAccessLogsMessage_TCPAccessLogEntriesValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = StreamAccessLogsMessage_TCPAccessLogEntriesValidationError{}
