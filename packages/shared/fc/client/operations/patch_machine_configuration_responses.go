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

// PatchMachineConfigurationReader is a Reader for the PatchMachineConfiguration structure.
type PatchMachineConfigurationReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *PatchMachineConfigurationReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {
	case 204:
		result := NewPatchMachineConfigurationNoContent()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil
	case 400:
		result := NewPatchMachineConfigurationBadRequest()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	default:
		result := NewPatchMachineConfigurationDefault(response.Code())
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		if response.Code()/100 == 2 {
			return result, nil
		}
		return nil, result
	}
}

// NewPatchMachineConfigurationNoContent creates a PatchMachineConfigurationNoContent with default headers values
func NewPatchMachineConfigurationNoContent() *PatchMachineConfigurationNoContent {
	return &PatchMachineConfigurationNoContent{}
}

/*
PatchMachineConfigurationNoContent describes a response with status code 204, with default header values.

Machine Configuration created/updated
*/
type PatchMachineConfigurationNoContent struct {
}

// IsSuccess returns true when this patch machine configuration no content response has a 2xx status code
func (o *PatchMachineConfigurationNoContent) IsSuccess() bool {
	return true
}

// IsRedirect returns true when this patch machine configuration no content response has a 3xx status code
func (o *PatchMachineConfigurationNoContent) IsRedirect() bool {
	return false
}

// IsClientError returns true when this patch machine configuration no content response has a 4xx status code
func (o *PatchMachineConfigurationNoContent) IsClientError() bool {
	return false
}

// IsServerError returns true when this patch machine configuration no content response has a 5xx status code
func (o *PatchMachineConfigurationNoContent) IsServerError() bool {
	return false
}

// IsCode returns true when this patch machine configuration no content response a status code equal to that given
func (o *PatchMachineConfigurationNoContent) IsCode(code int) bool {
	return code == 204
}

// Code gets the status code for the patch machine configuration no content response
func (o *PatchMachineConfigurationNoContent) Code() int {
	return 204
}

func (o *PatchMachineConfigurationNoContent) Error() string {
	return fmt.Sprintf("[PATCH /machine-config][%d] patchMachineConfigurationNoContent", 204)
}

func (o *PatchMachineConfigurationNoContent) String() string {
	return fmt.Sprintf("[PATCH /machine-config][%d] patchMachineConfigurationNoContent", 204)
}

func (o *PatchMachineConfigurationNoContent) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewPatchMachineConfigurationBadRequest creates a PatchMachineConfigurationBadRequest with default headers values
func NewPatchMachineConfigurationBadRequest() *PatchMachineConfigurationBadRequest {
	return &PatchMachineConfigurationBadRequest{}
}

/*
PatchMachineConfigurationBadRequest describes a response with status code 400, with default header values.

Machine Configuration cannot be updated due to bad input
*/
type PatchMachineConfigurationBadRequest struct {
	Payload *models.Error
}

// IsSuccess returns true when this patch machine configuration bad request response has a 2xx status code
func (o *PatchMachineConfigurationBadRequest) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this patch machine configuration bad request response has a 3xx status code
func (o *PatchMachineConfigurationBadRequest) IsRedirect() bool {
	return false
}

// IsClientError returns true when this patch machine configuration bad request response has a 4xx status code
func (o *PatchMachineConfigurationBadRequest) IsClientError() bool {
	return true
}

// IsServerError returns true when this patch machine configuration bad request response has a 5xx status code
func (o *PatchMachineConfigurationBadRequest) IsServerError() bool {
	return false
}

// IsCode returns true when this patch machine configuration bad request response a status code equal to that given
func (o *PatchMachineConfigurationBadRequest) IsCode(code int) bool {
	return code == 400
}

// Code gets the status code for the patch machine configuration bad request response
func (o *PatchMachineConfigurationBadRequest) Code() int {
	return 400
}

func (o *PatchMachineConfigurationBadRequest) Error() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PATCH /machine-config][%d] patchMachineConfigurationBadRequest %s", 400, payload)
}

func (o *PatchMachineConfigurationBadRequest) String() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PATCH /machine-config][%d] patchMachineConfigurationBadRequest %s", 400, payload)
}

func (o *PatchMachineConfigurationBadRequest) GetPayload() *models.Error {
	return o.Payload
}

func (o *PatchMachineConfigurationBadRequest) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewPatchMachineConfigurationDefault creates a PatchMachineConfigurationDefault with default headers values
func NewPatchMachineConfigurationDefault(code int) *PatchMachineConfigurationDefault {
	return &PatchMachineConfigurationDefault{
		_statusCode: code,
	}
}

/*
PatchMachineConfigurationDefault describes a response with status code -1, with default header values.

Internal server error
*/
type PatchMachineConfigurationDefault struct {
	_statusCode int

	Payload *models.Error
}

// IsSuccess returns true when this patch machine configuration default response has a 2xx status code
func (o *PatchMachineConfigurationDefault) IsSuccess() bool {
	return o._statusCode/100 == 2
}

// IsRedirect returns true when this patch machine configuration default response has a 3xx status code
func (o *PatchMachineConfigurationDefault) IsRedirect() bool {
	return o._statusCode/100 == 3
}

// IsClientError returns true when this patch machine configuration default response has a 4xx status code
func (o *PatchMachineConfigurationDefault) IsClientError() bool {
	return o._statusCode/100 == 4
}

// IsServerError returns true when this patch machine configuration default response has a 5xx status code
func (o *PatchMachineConfigurationDefault) IsServerError() bool {
	return o._statusCode/100 == 5
}

// IsCode returns true when this patch machine configuration default response a status code equal to that given
func (o *PatchMachineConfigurationDefault) IsCode(code int) bool {
	return o._statusCode == code
}

// Code gets the status code for the patch machine configuration default response
func (o *PatchMachineConfigurationDefault) Code() int {
	return o._statusCode
}

func (o *PatchMachineConfigurationDefault) Error() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PATCH /machine-config][%d] patchMachineConfiguration default %s", o._statusCode, payload)
}

func (o *PatchMachineConfigurationDefault) String() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PATCH /machine-config][%d] patchMachineConfiguration default %s", o._statusCode, payload)
}

func (o *PatchMachineConfigurationDefault) GetPayload() *models.Error {
	return o.Payload
}

func (o *PatchMachineConfigurationDefault) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}
