// Code generated by protoc-gen-validate. DO NOT EDIT.
// source: envoy/config/overload/v2alpha/overload.proto

package v2alpha

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

// Validate checks the field values on ResourceMonitor with the rules defined
// in the proto definition for this message. If any rules are violated, the
// first error encountered is returned, or nil if there are no violations.
func (m *ResourceMonitor) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on ResourceMonitor with the rules
// defined in the proto definition for this message. If any rules are
// violated, the result is a list of violation errors wrapped in
// ResourceMonitorMultiError, or nil if none found.
func (m *ResourceMonitor) ValidateAll() error {
	return m.validate(true)
}

func (m *ResourceMonitor) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	if len(m.GetName()) < 1 {
		err := ResourceMonitorValidationError{
			field:  "Name",
			reason: "value length must be at least 1 bytes",
		}
		if !all {
			return err
		}
		errors = append(errors, err)
	}

	switch m.ConfigType.(type) {

	case *ResourceMonitor_Config:

		if all {
			switch v := interface{}(m.GetConfig()).(type) {
			case interface{ ValidateAll() error }:
				if err := v.ValidateAll(); err != nil {
					errors = append(errors, ResourceMonitorValidationError{
						field:  "Config",
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			case interface{ Validate() error }:
				if err := v.Validate(); err != nil {
					errors = append(errors, ResourceMonitorValidationError{
						field:  "Config",
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			}
		} else if v, ok := interface{}(m.GetConfig()).(interface{ Validate() error }); ok {
			if err := v.Validate(); err != nil {
				return ResourceMonitorValidationError{
					field:  "Config",
					reason: "embedded message failed validation",
					cause:  err,
				}
			}
		}

	case *ResourceMonitor_TypedConfig:

		if all {
			switch v := interface{}(m.GetTypedConfig()).(type) {
			case interface{ ValidateAll() error }:
				if err := v.ValidateAll(); err != nil {
					errors = append(errors, ResourceMonitorValidationError{
						field:  "TypedConfig",
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			case interface{ Validate() error }:
				if err := v.Validate(); err != nil {
					errors = append(errors, ResourceMonitorValidationError{
						field:  "TypedConfig",
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			}
		} else if v, ok := interface{}(m.GetTypedConfig()).(interface{ Validate() error }); ok {
			if err := v.Validate(); err != nil {
				return ResourceMonitorValidationError{
					field:  "TypedConfig",
					reason: "embedded message failed validation",
					cause:  err,
				}
			}
		}

	}

	if len(errors) > 0 {
		return ResourceMonitorMultiError(errors)
	}

	return nil
}

// ResourceMonitorMultiError is an error wrapping multiple validation errors
// returned by ResourceMonitor.ValidateAll() if the designated constraints
// aren't met.
type ResourceMonitorMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m ResourceMonitorMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m ResourceMonitorMultiError) AllErrors() []error { return m }

// ResourceMonitorValidationError is the validation error returned by
// ResourceMonitor.Validate if the designated constraints aren't met.
type ResourceMonitorValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e ResourceMonitorValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e ResourceMonitorValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e ResourceMonitorValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e ResourceMonitorValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e ResourceMonitorValidationError) ErrorName() string { return "ResourceMonitorValidationError" }

// Error satisfies the builtin error interface
func (e ResourceMonitorValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sResourceMonitor.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = ResourceMonitorValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = ResourceMonitorValidationError{}

// Validate checks the field values on ThresholdTrigger with the rules defined
// in the proto definition for this message. If any rules are violated, the
// first error encountered is returned, or nil if there are no violations.
func (m *ThresholdTrigger) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on ThresholdTrigger with the rules
// defined in the proto definition for this message. If any rules are
// violated, the result is a list of violation errors wrapped in
// ThresholdTriggerMultiError, or nil if none found.
func (m *ThresholdTrigger) ValidateAll() error {
	return m.validate(true)
}

func (m *ThresholdTrigger) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	if val := m.GetValue(); val < 0 || val > 1 {
		err := ThresholdTriggerValidationError{
			field:  "Value",
			reason: "value must be inside range [0, 1]",
		}
		if !all {
			return err
		}
		errors = append(errors, err)
	}

	if len(errors) > 0 {
		return ThresholdTriggerMultiError(errors)
	}

	return nil
}

// ThresholdTriggerMultiError is an error wrapping multiple validation errors
// returned by ThresholdTrigger.ValidateAll() if the designated constraints
// aren't met.
type ThresholdTriggerMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m ThresholdTriggerMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m ThresholdTriggerMultiError) AllErrors() []error { return m }

// ThresholdTriggerValidationError is the validation error returned by
// ThresholdTrigger.Validate if the designated constraints aren't met.
type ThresholdTriggerValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e ThresholdTriggerValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e ThresholdTriggerValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e ThresholdTriggerValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e ThresholdTriggerValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e ThresholdTriggerValidationError) ErrorName() string { return "ThresholdTriggerValidationError" }

// Error satisfies the builtin error interface
func (e ThresholdTriggerValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sThresholdTrigger.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = ThresholdTriggerValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = ThresholdTriggerValidationError{}

// Validate checks the field values on Trigger with the rules defined in the
// proto definition for this message. If any rules are violated, the first
// error encountered is returned, or nil if there are no violations.
func (m *Trigger) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on Trigger with the rules defined in the
// proto definition for this message. If any rules are violated, the result is
// a list of violation errors wrapped in TriggerMultiError, or nil if none found.
func (m *Trigger) ValidateAll() error {
	return m.validate(true)
}

func (m *Trigger) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	if len(m.GetName()) < 1 {
		err := TriggerValidationError{
			field:  "Name",
			reason: "value length must be at least 1 bytes",
		}
		if !all {
			return err
		}
		errors = append(errors, err)
	}

	switch m.TriggerOneof.(type) {

	case *Trigger_Threshold:

		if all {
			switch v := interface{}(m.GetThreshold()).(type) {
			case interface{ ValidateAll() error }:
				if err := v.ValidateAll(); err != nil {
					errors = append(errors, TriggerValidationError{
						field:  "Threshold",
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			case interface{ Validate() error }:
				if err := v.Validate(); err != nil {
					errors = append(errors, TriggerValidationError{
						field:  "Threshold",
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			}
		} else if v, ok := interface{}(m.GetThreshold()).(interface{ Validate() error }); ok {
			if err := v.Validate(); err != nil {
				return TriggerValidationError{
					field:  "Threshold",
					reason: "embedded message failed validation",
					cause:  err,
				}
			}
		}

	default:
		err := TriggerValidationError{
			field:  "TriggerOneof",
			reason: "value is required",
		}
		if !all {
			return err
		}
		errors = append(errors, err)

	}

	if len(errors) > 0 {
		return TriggerMultiError(errors)
	}

	return nil
}

// TriggerMultiError is an error wrapping multiple validation errors returned
// by Trigger.ValidateAll() if the designated constraints aren't met.
type TriggerMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m TriggerMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m TriggerMultiError) AllErrors() []error { return m }

// TriggerValidationError is the validation error returned by Trigger.Validate
// if the designated constraints aren't met.
type TriggerValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e TriggerValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e TriggerValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e TriggerValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e TriggerValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e TriggerValidationError) ErrorName() string { return "TriggerValidationError" }

// Error satisfies the builtin error interface
func (e TriggerValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sTrigger.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = TriggerValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = TriggerValidationError{}

// Validate checks the field values on OverloadAction with the rules defined in
// the proto definition for this message. If any rules are violated, the first
// error encountered is returned, or nil if there are no violations.
func (m *OverloadAction) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on OverloadAction with the rules defined
// in the proto definition for this message. If any rules are violated, the
// result is a list of violation errors wrapped in OverloadActionMultiError,
// or nil if none found.
func (m *OverloadAction) ValidateAll() error {
	return m.validate(true)
}

func (m *OverloadAction) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	if len(m.GetName()) < 1 {
		err := OverloadActionValidationError{
			field:  "Name",
			reason: "value length must be at least 1 bytes",
		}
		if !all {
			return err
		}
		errors = append(errors, err)
	}

	if len(m.GetTriggers()) < 1 {
		err := OverloadActionValidationError{
			field:  "Triggers",
			reason: "value must contain at least 1 item(s)",
		}
		if !all {
			return err
		}
		errors = append(errors, err)
	}

	for idx, item := range m.GetTriggers() {
		_, _ = idx, item

		if all {
			switch v := interface{}(item).(type) {
			case interface{ ValidateAll() error }:
				if err := v.ValidateAll(); err != nil {
					errors = append(errors, OverloadActionValidationError{
						field:  fmt.Sprintf("Triggers[%v]", idx),
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			case interface{ Validate() error }:
				if err := v.Validate(); err != nil {
					errors = append(errors, OverloadActionValidationError{
						field:  fmt.Sprintf("Triggers[%v]", idx),
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			}
		} else if v, ok := interface{}(item).(interface{ Validate() error }); ok {
			if err := v.Validate(); err != nil {
				return OverloadActionValidationError{
					field:  fmt.Sprintf("Triggers[%v]", idx),
					reason: "embedded message failed validation",
					cause:  err,
				}
			}
		}

	}

	if len(errors) > 0 {
		return OverloadActionMultiError(errors)
	}

	return nil
}

// OverloadActionMultiError is an error wrapping multiple validation errors
// returned by OverloadAction.ValidateAll() if the designated constraints
// aren't met.
type OverloadActionMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m OverloadActionMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m OverloadActionMultiError) AllErrors() []error { return m }

// OverloadActionValidationError is the validation error returned by
// OverloadAction.Validate if the designated constraints aren't met.
type OverloadActionValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e OverloadActionValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e OverloadActionValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e OverloadActionValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e OverloadActionValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e OverloadActionValidationError) ErrorName() string { return "OverloadActionValidationError" }

// Error satisfies the builtin error interface
func (e OverloadActionValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sOverloadAction.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = OverloadActionValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = OverloadActionValidationError{}

// Validate checks the field values on OverloadManager with the rules defined
// in the proto definition for this message. If any rules are violated, the
// first error encountered is returned, or nil if there are no violations.
func (m *OverloadManager) Validate() error {
	return m.validate(false)
}

// ValidateAll checks the field values on OverloadManager with the rules
// defined in the proto definition for this message. If any rules are
// violated, the result is a list of violation errors wrapped in
// OverloadManagerMultiError, or nil if none found.
func (m *OverloadManager) ValidateAll() error {
	return m.validate(true)
}

func (m *OverloadManager) validate(all bool) error {
	if m == nil {
		return nil
	}

	var errors []error

	if all {
		switch v := interface{}(m.GetRefreshInterval()).(type) {
		case interface{ ValidateAll() error }:
			if err := v.ValidateAll(); err != nil {
				errors = append(errors, OverloadManagerValidationError{
					field:  "RefreshInterval",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		case interface{ Validate() error }:
			if err := v.Validate(); err != nil {
				errors = append(errors, OverloadManagerValidationError{
					field:  "RefreshInterval",
					reason: "embedded message failed validation",
					cause:  err,
				})
			}
		}
	} else if v, ok := interface{}(m.GetRefreshInterval()).(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return OverloadManagerValidationError{
				field:  "RefreshInterval",
				reason: "embedded message failed validation",
				cause:  err,
			}
		}
	}

	if len(m.GetResourceMonitors()) < 1 {
		err := OverloadManagerValidationError{
			field:  "ResourceMonitors",
			reason: "value must contain at least 1 item(s)",
		}
		if !all {
			return err
		}
		errors = append(errors, err)
	}

	for idx, item := range m.GetResourceMonitors() {
		_, _ = idx, item

		if all {
			switch v := interface{}(item).(type) {
			case interface{ ValidateAll() error }:
				if err := v.ValidateAll(); err != nil {
					errors = append(errors, OverloadManagerValidationError{
						field:  fmt.Sprintf("ResourceMonitors[%v]", idx),
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			case interface{ Validate() error }:
				if err := v.Validate(); err != nil {
					errors = append(errors, OverloadManagerValidationError{
						field:  fmt.Sprintf("ResourceMonitors[%v]", idx),
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			}
		} else if v, ok := interface{}(item).(interface{ Validate() error }); ok {
			if err := v.Validate(); err != nil {
				return OverloadManagerValidationError{
					field:  fmt.Sprintf("ResourceMonitors[%v]", idx),
					reason: "embedded message failed validation",
					cause:  err,
				}
			}
		}

	}

	for idx, item := range m.GetActions() {
		_, _ = idx, item

		if all {
			switch v := interface{}(item).(type) {
			case interface{ ValidateAll() error }:
				if err := v.ValidateAll(); err != nil {
					errors = append(errors, OverloadManagerValidationError{
						field:  fmt.Sprintf("Actions[%v]", idx),
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			case interface{ Validate() error }:
				if err := v.Validate(); err != nil {
					errors = append(errors, OverloadManagerValidationError{
						field:  fmt.Sprintf("Actions[%v]", idx),
						reason: "embedded message failed validation",
						cause:  err,
					})
				}
			}
		} else if v, ok := interface{}(item).(interface{ Validate() error }); ok {
			if err := v.Validate(); err != nil {
				return OverloadManagerValidationError{
					field:  fmt.Sprintf("Actions[%v]", idx),
					reason: "embedded message failed validation",
					cause:  err,
				}
			}
		}

	}

	if len(errors) > 0 {
		return OverloadManagerMultiError(errors)
	}

	return nil
}

// OverloadManagerMultiError is an error wrapping multiple validation errors
// returned by OverloadManager.ValidateAll() if the designated constraints
// aren't met.
type OverloadManagerMultiError []error

// Error returns a concatenation of all the error messages it wraps.
func (m OverloadManagerMultiError) Error() string {
	var msgs []string
	for _, err := range m {
		msgs = append(msgs, err.Error())
	}
	return strings.Join(msgs, "; ")
}

// AllErrors returns a list of validation violation errors.
func (m OverloadManagerMultiError) AllErrors() []error { return m }

// OverloadManagerValidationError is the validation error returned by
// OverloadManager.Validate if the designated constraints aren't met.
type OverloadManagerValidationError struct {
	field  string
	reason string
	cause  error
	key    bool
}

// Field function returns field value.
func (e OverloadManagerValidationError) Field() string { return e.field }

// Reason function returns reason value.
func (e OverloadManagerValidationError) Reason() string { return e.reason }

// Cause function returns cause value.
func (e OverloadManagerValidationError) Cause() error { return e.cause }

// Key function returns key value.
func (e OverloadManagerValidationError) Key() bool { return e.key }

// ErrorName returns error name.
func (e OverloadManagerValidationError) ErrorName() string { return "OverloadManagerValidationError" }

// Error satisfies the builtin error interface
func (e OverloadManagerValidationError) Error() string {
	cause := ""
	if e.cause != nil {
		cause = fmt.Sprintf(" | caused by: %v", e.cause)
	}

	key := ""
	if e.key {
		key = "key for "
	}

	return fmt.Sprintf(
		"invalid %sOverloadManager.%s: %s%s",
		key,
		e.field,
		e.reason,
		cause)
}

var _ error = OverloadManagerValidationError{}

var _ interface {
	Field() string
	Reason() string
	Key() bool
	Cause() error
	ErrorName() string
} = OverloadManagerValidationError{}