// Code generated by protoc-gen-validate. DO NOT EDIT.
// source: envoy/config/filter/network/dubbo_proxy/v2alpha1/dubbo_proxy.proto

package v2alpha1

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

// Validate checks the field values on DubboProxy with the rules defined in the
// proto definition for this message. If any rules are violated, the first
// error encountered is returned, or nil if there are no violations.
func (m *DubboProxy) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on DubboProxy with the rules defined in
// the proto definition for this message. If any rules are violated, the
// result is a list of violation errors wrapped in DubboProxyMultiError, or
// nil if none found.
func (m *DubboProxy) ValidateAll() error {
	return m.validate(true)
}

func (m *DubboProxy) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	if len(m.GetStatPrefix()) < 1 {
		err := DubboProxyValidationError{
			field:  "StatPrefix",
			reason: "value length must be at least 1 bytes",
		}
		if !all {
			return err
		}
		errors = append(errors, err)
	}

	if _, ok := ProtocolType_name[int32(m.GetProtocolType())]; !ok {
		err := DubboProxyValidationError{
			field:  "ProtocolType",
			reason: "value must be one of the defined enum values",
		}
		if !all {
			return err
		}
		errors = append(errors, err)
	}

	if _, ok := SerializationType_name[int32(m.GetSerializationType())]; !ok {
		err := DubboProxyValidationError{
			field:  "SerializationType",
			reason: "value must be one of the defined enum values",
		}
		if !all {
			return err
		}
		errors = append(errors, err)
	}

	for idx, item := range m.GetRouteConfig() {
		_, _ = idx, item

		if all {
			switch v := interface{}(item).(type) {
			case interface{ ValidateAll() error }:
				if err := v.ValidateAll(); err != nil {
					errors = append(errors, DubboProxyValidationError{
						field:  fmt.Sprintf("RouteConfig[%v]", idx),
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			case interface{ Validate() error }:
				if err := v.Validate(); err != nil {
					errors = append(errors, DubboProxyValidationError{
						field:  fmt.Sprintf("RouteConfig[%v]", idx),
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			}
		} else if v, ok := interface{}(item).(interface{ Validate() error }); ok {
			if err := v.Validate(); err != nil {
				return DubboProxyValidationError{
					field:  fmt.Sprintf("RouteConfig[%v]", idx),
					reason: "embedded message failed validation",
					cause:  err,
				}
			}
		}

	}

	for idx, item := range m.GetDubboFilters() {
		_, _ = idx, item

		if all {
			switch v := interface{}(item).(type) {
			case interface{ ValidateAll() error }:
				if err := v.ValidateAll(); err != nil {
					errors = append(errors, DubboProxyValidationError{
						field:  fmt.Sprintf("DubboFilters[%v]", idx),
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			case interface{ Validate() error }:
				if err := v.Validate(); err != nil {
					errors = append(errors, DubboProxyValidationError{
						field:  fmt.Sprintf("DubboFilters[%v]", idx),
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			}
		} else if v, ok := interface{}(item).(interface{ Validate() error }); ok {
			if err := v.Validate(); err != nil {
				return DubboProxyValidationError{
					field:  fmt.Sprintf("DubboFilters[%v]", idx),
					reason: "embedded message failed validation",
					cause:  err,
				}
			}
		}

	}

	if len(errors) > 0 {
		return DubboProxyMultiError(errors)
	}

	return nil
}

// DubboProxyMultiError is an error wrapping multiple validation errors
// returned by DubboProxy.ValidateAll() if the designated constraints aren't met.
type DubboProxyMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m DubboProxyMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m DubboProxyMultiError) AllErrors() []error { return m }

// DubboProxyValidationError is the validation error returned by
// DubboProxy.Validate if the designated constraints aren't met.
type DubboProxyValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e DubboProxyValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e DubboProxyValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e DubboProxyValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e DubboProxyValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e DubboProxyValidationError) ErrorName() string { return "DubboProxyValidationError" }

// Error satisfies the builtin error interface
func (e DubboProxyValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sDubboProxy.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = DubboProxyValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = DubboProxyValidationError{}

// Validate checks the field values on DubboFilter with the rules defined in
// the proto definition for this message. If any rules are violated, the first
// error encountered is returned, or nil if there are no violations.
func (m *DubboFilter) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on DubboFilter with the rules defined in
// the proto definition for this message. If any rules are violated, the
// result is a list of violation errors wrapped in DubboFilterMultiError, or
// nil if none found.
func (m *DubboFilter) ValidateAll() error {
	return m.validate(true)
}

func (m *DubboFilter) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	if len(m.GetName()) < 1 {
		err := DubboFilterValidationError{
			field:  "Name",
			reason: "value length must be at least 1 bytes",
		}
		if !all {
			return err
		}
		errors = append(errors, err)
	}

	if all {
		switch v := interface{}(m.GetConfig()).(type) {
		case interface{ ValidateAll() error }:
			if err := v.ValidateAll(); err != nil {
				errors = append(errors, DubboFilterValidationError{
					field:  "Config",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		case interface{ Validate() error }:
			if err := v.Validate(); err != nil {
				errors = append(errors, DubboFilterValidationError{
					field:  "Config",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		}
	} else if v, ok := interface{}(m.GetConfig()).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return DubboFilterValidationError{
				field:  "Config",
				reason: "embedded message failed validation",
				cause:  err,
			}
		}
	}

	if len(errors) > 0 {
		return DubboFilterMultiError(errors)
	}

	return nil
}

// DubboFilterMultiError is an error wrapping multiple validation errors
// returned by DubboFilter.ValidateAll() if the designated constraints aren't met.
type DubboFilterMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m DubboFilterMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m DubboFilterMultiError) AllErrors() []error { return m }

// DubboFilterValidationError is the validation error returned by
// DubboFilter.Validate if the designated constraints aren't met.
type DubboFilterValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e DubboFilterValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e DubboFilterValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e DubboFilterValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e DubboFilterValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e DubboFilterValidationError) ErrorName() string { return "DubboFilterValidationError" }

// Error satisfies the builtin error interface
func (e DubboFilterValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sDubboFilter.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = DubboFilterValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = DubboFilterValidationError{}
