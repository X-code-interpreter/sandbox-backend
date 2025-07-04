// Code generated by go-swagger; DO NOT EDIT.

package operations

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/fc/models"
)

// PatchMmdsReader is a Reader for the PatchMmds structure.
type PatchMmdsReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *PatchMmdsReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {
	case 204:
		result := NewPatchMmdsNoContent()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil
	case 400:
		result := NewPatchMmdsBadRequest()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	default:
		result := NewPatchMmdsDefault(response.Code())
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		if response.Code()/100 == 2 {
			return result, nil
		}
		return nil, result
	}
}

// NewPatchMmdsNoContent creates a PatchMmdsNoContent with default headers values
func NewPatchMmdsNoContent() *PatchMmdsNoContent {
	return &PatchMmdsNoContent{}
}

/*
PatchMmdsNoContent describes a response with status code 204, with default header values.

MMDS data store updated.
*/
type PatchMmdsNoContent struct {
}

// IsSuccess returns true when this patch mmds no content response has a 2xx status code
func (o *PatchMmdsNoContent) IsSuccess() bool {
	return true
}

// IsRedirect returns true when this patch mmds no content response has a 3xx status code
func (o *PatchMmdsNoContent) IsRedirect() bool {
	return false
}

// IsClientError returns true when this patch mmds no content response has a 4xx status code
func (o *PatchMmdsNoContent) IsClientError() bool {
	return false
}

// IsServerError returns true when this patch mmds no content response has a 5xx status code
func (o *PatchMmdsNoContent) IsServerError() bool {
	return false
}

// IsCode returns true when this patch mmds no content response a status code equal to that given
func (o *PatchMmdsNoContent) IsCode(code int) bool {
	return code == 204
}

// Code gets the status code for the patch mmds no content response
func (o *PatchMmdsNoContent) Code() int {
	return 204
}

func (o *PatchMmdsNoContent) Error() string {
	return fmt.Sprintf("[PATCH /mmds][%d] patchMmdsNoContent", 204)
}

func (o *PatchMmdsNoContent) String() string {
	return fmt.Sprintf("[PATCH /mmds][%d] patchMmdsNoContent", 204)
}

func (o *PatchMmdsNoContent) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewPatchMmdsBadRequest creates a PatchMmdsBadRequest with default headers values
func NewPatchMmdsBadRequest() *PatchMmdsBadRequest {
	return &PatchMmdsBadRequest{}
}

/*
PatchMmdsBadRequest describes a response with status code 400, with default header values.

MMDS data store cannot be updated due to bad input.
*/
type PatchMmdsBadRequest struct {
	Payload *models.Error
}

// IsSuccess returns true when this patch mmds bad request response has a 2xx status code
func (o *PatchMmdsBadRequest) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this patch mmds bad request response has a 3xx status code
func (o *PatchMmdsBadRequest) IsRedirect() bool {
	return false
}

// IsClientError returns true when this patch mmds bad request response has a 4xx status code
func (o *PatchMmdsBadRequest) IsClientError() bool {
	return true
}

// IsServerError returns true when this patch mmds bad request response has a 5xx status code
func (o *PatchMmdsBadRequest) IsServerError() bool {
	return false
}

// IsCode returns true when this patch mmds bad request response a status code equal to that given
func (o *PatchMmdsBadRequest) IsCode(code int) bool {
	return code == 400
}

// Code gets the status code for the patch mmds bad request response
func (o *PatchMmdsBadRequest) Code() int {
	return 400
}

func (o *PatchMmdsBadRequest) Error() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PATCH /mmds][%d] patchMmdsBadRequest %s", 400, payload)
}

func (o *PatchMmdsBadRequest) String() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PATCH /mmds][%d] patchMmdsBadRequest %s", 400, payload)
}

func (o *PatchMmdsBadRequest) GetPayload() *models.Error {
	return o.Payload
}

func (o *PatchMmdsBadRequest) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewPatchMmdsDefault creates a PatchMmdsDefault with default headers values
func NewPatchMmdsDefault(code int) *PatchMmdsDefault {
	return &PatchMmdsDefault{
		_statusCode: code,
	}
}

/*
PatchMmdsDefault describes a response with status code -1, with default header values.

Internal server error
*/
type PatchMmdsDefault struct {
	_statusCode int

	Payload *models.Error
}

// IsSuccess returns true when this patch mmds default response has a 2xx status code
func (o *PatchMmdsDefault) IsSuccess() bool {
	return o._statusCode/100 == 2
}

// IsRedirect returns true when this patch mmds default response has a 3xx status code
func (o *PatchMmdsDefault) IsRedirect() bool {
	return o._statusCode/100 == 3
}

// IsClientError returns true when this patch mmds default response has a 4xx status code
func (o *PatchMmdsDefault) IsClientError() bool {
	return o._statusCode/100 == 4
}

// IsServerError returns true when this patch mmds default response has a 5xx status code
func (o *PatchMmdsDefault) IsServerError() bool {
	return o._statusCode/100 == 5
}

// IsCode returns true when this patch mmds default response a status code equal to that given
func (o *PatchMmdsDefault) IsCode(code int) bool {
	return o._statusCode == code
}

// Code gets the status code for the patch mmds default response
func (o *PatchMmdsDefault) Code() int {
	return o._statusCode
}

func (o *PatchMmdsDefault) Error() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PATCH /mmds][%d] patchMmds default %s", o._statusCode, payload)
}

func (o *PatchMmdsDefault) String() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PATCH /mmds][%d] patchMmds default %s", o._statusCode, payload)
}

func (o *PatchMmdsDefault) GetPayload() *models.Error {
	return o.Payload
}

func (o *PatchMmdsDefault) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}
