/*
 *
 * Copyright 2017 gRPC authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 */

// Package status implements errors returned by gRPC.  These errors are
// serialized and transmitted on the wire between server and client, and allow
// for additional data to be transmitted via the Details field in the status
// proto.  gRPC service handlers should return an error created by this
// package, and gRPC clients should expect a corresponding error to be
// returned from the RPC call.
//
// This package upholds the invariants that a non-nil error may not
// contain an OK code, and an OK code must result in a nil error.
package status

import (
	"errors"
	"fmt"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	spb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	any "google.golang.org/protobuf/types/known/anypb"

	rpc "istio.io/gogo-genproto/googleapis/google/rpc"
)

// statusError is an alias of a status proto.  It implements error and Status,
// and a nil statusError should never be returned by this package.
type statusError rpc.Status

func (se *statusError) Error() string {
	p := (*rpc.Status)(se)
	return fmt.Sprintf("rpc error: code = %s desc = %s", codes.Code(p.GetCode()), p.GetMessage())
}

// GRPCStatus converts the gogo/statusError to a grpc/status.
func (se *statusError) GRPCStatus() *status.Status {
	p := (*rpc.Status)(se)
	s := &spb.Status{
		Code:    p.GetCode(),
		Message: p.GetMessage(),
	}
	for _, detail := range p.GetDetails() {
		s.Details = append(s.GetDetails(), &any.Any{
			TypeUrl: detail.GetTypeUrl(),
			Value:   detail.GetValue(),
		})
	}
	return status.FromProto(s)
}

// Status represents an RPC status code, message, and details.  It is immutable
// and should be created with New, Newf, or FromProto.
type Status struct {
	s *rpc.Status
}

// Code returns the status code contained in s.
func (s *Status) Code() codes.Code {
	if s == nil || s.s == nil {
		return codes.OK
	}
	return codes.Code(s.s.Code)
}

// Message returns the message contained in s.
func (s *Status) Message() string {
	if s == nil || s.s == nil {
		return ""
	}
	return s.s.Message
}

// Proto returns s's status as an rpc.Status proto message.
func (s *Status) Proto() *rpc.Status {
	if s == nil {
		return nil
	}
	return proto.Clone(s.s).(*rpc.Status)
}

// Err returns an immutable error representing s; returns nil if s.Code() is
// OK.
func (s *Status) Err() error {
	if s.Code() == codes.OK {
		return nil
	}
	return (*statusError)(s.s)
}

// New returns a Status representing c and msg.
func New(c codes.Code, msg string) *Status {
	return &Status{s: &rpc.Status{Code: int32(c), Message: msg}}
}

// Newf returns New(c, fmt.Sprintf(format, a...)).
func Newf(c codes.Code, format string, a ...interface{}) *Status {
	return New(c, fmt.Sprintf(format, a...))
}

// Error returns an error representing c and msg.  If c is OK, returns nil.
func Error(c codes.Code, msg string) error {
	return New(c, msg).Err()
}

// Errorf returns Error(c, fmt.Sprintf(format, a...)).
func Errorf(c codes.Code, format string, a ...interface{}) error {
	return Error(c, fmt.Sprintf(format, a...))
}

// ErrorProto returns an error representing s.  If s.Code is OK, returns nil.
func ErrorProto(s *rpc.Status) error {
	return FromProto(s).Err()
}

// FromProto returns a Status representing s.
func FromProto(s *rpc.Status) *Status {
	return &Status{s: proto.Clone(s).(*rpc.Status)}
}

// FromError returns a Status representing err if it was produced from this
// package or the standard grpc/status package. Otherwise, ok is false and
// a Status is returned with codes.Unknown and the original error message.
func FromError(err error) (s *Status, ok bool) {
	if err == nil {
		return &Status{s: &rpc.Status{Code: int32(codes.OK)}}, true
	}
	if se, ok := err.(interface{ GRPCStatus() *status.Status }); ok {
		return FromGRPCStatus(se.GRPCStatus()), true
	}
	return New(codes.Unknown, err.Error()), false
}

// FromGRPCStatus converts a grpc.Status to gogo.Status.
func FromGRPCStatus(st *status.Status) *Status {
	p := st.Proto()
	pb := &rpc.Status{
		Code:    p.GetCode(),
		Message: p.GetMessage(),
	}
	for _, detail := range p.GetDetails() {
		pb.Details = append(pb.GetDetails(), &types.Any{
			TypeUrl: detail.GetTypeUrl(),
			Value:   detail.GetValue(),
		})
	}
	return FromProto(pb)
}

// Convert is a convenience function which removes the need to handle the
// boolean return value from FromError.
func Convert(err error) *Status {
	s, _ := FromError(err)
	return s
}

// WithDetails returns a new status with the provided details messages appended to the status.
// If any errors are encountered, it returns nil and the first error encountered.
func (s *Status) WithDetails(details ...proto.Message) (*Status, error) {
	if s.Code() == codes.OK {
		return nil, errors.New("no error details for status with code OK")
	}
	// s.Code() != OK implies that s.Proto() != nil.
	p := s.Proto()
	for _, detail := range details {
		body, err := types.MarshalAny(detail)
		if err != nil {
			return nil, err
		}
		p.Details = append(p.Details, body)
	}
	return &Status{s: p}, nil
}

// Details returns a slice of details messages attached to the status.
// If a detail cannot be decoded, the error is returned in place of the detail.
func (s *Status) Details() []interface{} {
	if s == nil || s.s == nil {
		return nil
	}
	details := make([]interface{}, 0, len(s.s.Details))
	for _, body := range s.s.Details {
		detail := &types.DynamicAny{}
		if err := types.UnmarshalAny(body, detail); err != nil {
			details = append(details, err)
			continue
		}
		details = append(details, detail.Message)
	}
	return details
}

// Code returns the Code of the error if it is a Status error, codes.OK if err
// is nil, or codes.Unknown otherwise.
func Code(err error) codes.Code {
	// Don't use FromError to avoid allocation of OK status.
	if err == nil {
		return codes.OK
	}
	if se, ok := err.(interface{ GRPCStatus() *status.Status }); ok {
		return se.GRPCStatus().Code()
	}
	return codes.Unknown
}
