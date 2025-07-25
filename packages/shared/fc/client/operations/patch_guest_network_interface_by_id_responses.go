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

// PatchGuestNetworkInterfaceByIDReader is a Reader for the PatchGuestNetworkInterfaceByID structure.
type PatchGuestNetworkInterfaceByIDReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *PatchGuestNetworkInterfaceByIDReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {
	case 204:
		result := NewPatchGuestNetworkInterfaceByIDNoContent()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil
	case 400:
		result := NewPatchGuestNetworkInterfaceByIDBadRequest()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	default:
		result := NewPatchGuestNetworkInterfaceByIDDefault(response.Code())
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		if response.Code()/100 == 2 {
			return result, nil
		}
		return nil, result
	}
}

// NewPatchGuestNetworkInterfaceByIDNoContent creates a PatchGuestNetworkInterfaceByIDNoContent with default headers values
func NewPatchGuestNetworkInterfaceByIDNoContent() *PatchGuestNetworkInterfaceByIDNoContent {
	return &PatchGuestNetworkInterfaceByIDNoContent{}
}

/*
PatchGuestNetworkInterfaceByIDNoContent describes a response with status code 204, with default header values.

Network interface updated
*/
type PatchGuestNetworkInterfaceByIDNoContent struct {
}

// IsSuccess returns true when this patch guest network interface by Id no content response has a 2xx status code
func (o *PatchGuestNetworkInterfaceByIDNoContent) IsSuccess() bool {
	return true
}

// IsRedirect returns true when this patch guest network interface by Id no content response has a 3xx status code
func (o *PatchGuestNetworkInterfaceByIDNoContent) IsRedirect() bool {
	return false
}

// IsClientError returns true when this patch guest network interface by Id no content response has a 4xx status code
func (o *PatchGuestNetworkInterfaceByIDNoContent) IsClientError() bool {
	return false
}

// IsServerError returns true when this patch guest network interface by Id no content response has a 5xx status code
func (o *PatchGuestNetworkInterfaceByIDNoContent) IsServerError() bool {
	return false
}

// IsCode returns true when this patch guest network interface by Id no content response a status code equal to that given
func (o *PatchGuestNetworkInterfaceByIDNoContent) IsCode(code int) bool {
	return code == 204
}

// Code gets the status code for the patch guest network interface by Id no content response
func (o *PatchGuestNetworkInterfaceByIDNoContent) Code() int {
	return 204
}

func (o *PatchGuestNetworkInterfaceByIDNoContent) Error() string {
	return fmt.Sprintf("[PATCH /network-interfaces/{iface_id}][%d] patchGuestNetworkInterfaceByIdNoContent", 204)
}

func (o *PatchGuestNetworkInterfaceByIDNoContent) String() string {
	return fmt.Sprintf("[PATCH /network-interfaces/{iface_id}][%d] patchGuestNetworkInterfaceByIdNoContent", 204)
}

func (o *PatchGuestNetworkInterfaceByIDNoContent) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewPatchGuestNetworkInterfaceByIDBadRequest creates a PatchGuestNetworkInterfaceByIDBadRequest with default headers values
func NewPatchGuestNetworkInterfaceByIDBadRequest() *PatchGuestNetworkInterfaceByIDBadRequest {
	return &PatchGuestNetworkInterfaceByIDBadRequest{}
}

/*
PatchGuestNetworkInterfaceByIDBadRequest describes a response with status code 400, with default header values.

Network interface cannot be updated due to bad input
*/
type PatchGuestNetworkInterfaceByIDBadRequest struct {
	Payload *models.Error
}

// IsSuccess returns true when this patch guest network interface by Id bad request response has a 2xx status code
func (o *PatchGuestNetworkInterfaceByIDBadRequest) IsSuccess() bool {
	return false
}

// IsRedirect returns true when this patch guest network interface by Id bad request response has a 3xx status code
func (o *PatchGuestNetworkInterfaceByIDBadRequest) IsRedirect() bool {
	return false
}

// IsClientError returns true when this patch guest network interface by Id bad request response has a 4xx status code
func (o *PatchGuestNetworkInterfaceByIDBadRequest) IsClientError() bool {
	return true
}

// IsServerError returns true when this patch guest network interface by Id bad request response has a 5xx status code
func (o *PatchGuestNetworkInterfaceByIDBadRequest) IsServerError() bool {
	return false
}

// IsCode returns true when this patch guest network interface by Id bad request response a status code equal to that given
func (o *PatchGuestNetworkInterfaceByIDBadRequest) IsCode(code int) bool {
	return code == 400
}

// Code gets the status code for the patch guest network interface by Id bad request response
func (o *PatchGuestNetworkInterfaceByIDBadRequest) Code() int {
	return 400
}

func (o *PatchGuestNetworkInterfaceByIDBadRequest) Error() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PATCH /network-interfaces/{iface_id}][%d] patchGuestNetworkInterfaceByIdBadRequest %s", 400, payload)
}

func (o *PatchGuestNetworkInterfaceByIDBadRequest) String() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PATCH /network-interfaces/{iface_id}][%d] patchGuestNetworkInterfaceByIdBadRequest %s", 400, payload)
}

func (o *PatchGuestNetworkInterfaceByIDBadRequest) GetPayload() *models.Error {
	return o.Payload
}

func (o *PatchGuestNetworkInterfaceByIDBadRequest) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewPatchGuestNetworkInterfaceByIDDefault creates a PatchGuestNetworkInterfaceByIDDefault with default headers values
func NewPatchGuestNetworkInterfaceByIDDefault(code int) *PatchGuestNetworkInterfaceByIDDefault {
	return &PatchGuestNetworkInterfaceByIDDefault{
		_statusCode: code,
	}
}

/*
PatchGuestNetworkInterfaceByIDDefault describes a response with status code -1, with default header values.

Internal server error
*/
type PatchGuestNetworkInterfaceByIDDefault struct {
	_statusCode int

	Payload *models.Error
}

// IsSuccess returns true when this patch guest network interface by ID default response has a 2xx status code
func (o *PatchGuestNetworkInterfaceByIDDefault) IsSuccess() bool {
	return o._statusCode/100 == 2
}

// IsRedirect returns true when this patch guest network interface by ID default response has a 3xx status code
func (o *PatchGuestNetworkInterfaceByIDDefault) IsRedirect() bool {
	return o._statusCode/100 == 3
}

// IsClientError returns true when this patch guest network interface by ID default response has a 4xx status code
func (o *PatchGuestNetworkInterfaceByIDDefault) IsClientError() bool {
	return o._statusCode/100 == 4
}

// IsServerError returns true when this patch guest network interface by ID default response has a 5xx status code
func (o *PatchGuestNetworkInterfaceByIDDefault) IsServerError() bool {
	return o._statusCode/100 == 5
}

// IsCode returns true when this patch guest network interface by ID default response a status code equal to that given
func (o *PatchGuestNetworkInterfaceByIDDefault) IsCode(code int) bool {
	return o._statusCode == code
}

// Code gets the status code for the patch guest network interface by ID default response
func (o *PatchGuestNetworkInterfaceByIDDefault) Code() int {
	return o._statusCode
}

func (o *PatchGuestNetworkInterfaceByIDDefault) Error() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PATCH /network-interfaces/{iface_id}][%d] patchGuestNetworkInterfaceByID default %s", o._statusCode, payload)
}

func (o *PatchGuestNetworkInterfaceByIDDefault) String() string {
	payload, _ := json.Marshal(o.Payload)
	return fmt.Sprintf("[PATCH /network-interfaces/{iface_id}][%d] patchGuestNetworkInterfaceByID default %s", o._statusCode, payload)
}

func (o *PatchGuestNetworkInterfaceByIDDefault) GetPayload() *models.Error {
	return o.Payload
}

func (o *PatchGuestNetworkInterfaceByIDDefault) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}
