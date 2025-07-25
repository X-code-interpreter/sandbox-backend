// Code generated by go-swagger; DO NOT EDIT.

package operations

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"context"
	"net/http"
	"time"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"
	cr "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"

	"github.com/X-code-interpreter/sandbox-backend/packages/shared/fc/models"
)

// NewPutLoggerParams creates a new PutLoggerParams object,
// with the default timeout for this client.
//
// Default values are not hydrated, since defaults are normally applied by the API server side.
//
// To enforce default values in parameter, use SetDefaults or WithDefaults.
func NewPutLoggerParams() *PutLoggerParams {
	return &PutLoggerParams{
		timeout: cr.DefaultTimeout,
	}
}

// NewPutLoggerParamsWithTimeout creates a new PutLoggerParams object
// with the ability to set a timeout on a request.
func NewPutLoggerParamsWithTimeout(timeout time.Duration) *PutLoggerParams {
	return &PutLoggerParams{
		timeout: timeout,
	}
}

// NewPutLoggerParamsWithContext creates a new PutLoggerParams object
// with the ability to set a context for a request.
func NewPutLoggerParamsWithContext(ctx context.Context) *PutLoggerParams {
	return &PutLoggerParams{
		Context: ctx,
	}
}

// NewPutLoggerParamsWithHTTPClient creates a new PutLoggerParams object
// with the ability to set a custom HTTPClient for a request.
func NewPutLoggerParamsWithHTTPClient(client *http.Client) *PutLoggerParams {
	return &PutLoggerParams{
		HTTPClient: client,
	}
}

/*
PutLoggerParams contains all the parameters to send to the API endpoint

	for the put logger operation.

	Typically these are written to a http.Request.
*/
type PutLoggerParams struct {

	/* Body.

	   Logging system description
	*/
	Body *models.Logger

	timeout    time.Duration
	Context    context.Context
	HTTPClient *http.Client
}

// WithDefaults hydrates default values in the put logger params (not the query body).
//
// All values with no default are reset to their zero value.
func (o *PutLoggerParams) WithDefaults() *PutLoggerParams {
	o.SetDefaults()
	return o
}

// SetDefaults hydrates default values in the put logger params (not the query body).
//
// All values with no default are reset to their zero value.
func (o *PutLoggerParams) SetDefaults() {
	// no default values defined for this parameter
}

// WithTimeout adds the timeout to the put logger params
func (o *PutLoggerParams) WithTimeout(timeout time.Duration) *PutLoggerParams {
	o.SetTimeout(timeout)
	return o
}

// SetTimeout adds the timeout to the put logger params
func (o *PutLoggerParams) SetTimeout(timeout time.Duration) {
	o.timeout = timeout
}

// WithContext adds the context to the put logger params
func (o *PutLoggerParams) WithContext(ctx context.Context) *PutLoggerParams {
	o.SetContext(ctx)
	return o
}

// SetContext adds the context to the put logger params
func (o *PutLoggerParams) SetContext(ctx context.Context) {
	o.Context = ctx
}

// WithHTTPClient adds the HTTPClient to the put logger params
func (o *PutLoggerParams) WithHTTPClient(client *http.Client) *PutLoggerParams {
	o.SetHTTPClient(client)
	return o
}

// SetHTTPClient adds the HTTPClient to the put logger params
func (o *PutLoggerParams) SetHTTPClient(client *http.Client) {
	o.HTTPClient = client
}

// WithBody adds the body to the put logger params
func (o *PutLoggerParams) WithBody(body *models.Logger) *PutLoggerParams {
	o.SetBody(body)
	return o
}

// SetBody adds the body to the put logger params
func (o *PutLoggerParams) SetBody(body *models.Logger) {
	o.Body = body
}

// WriteToRequest writes these params to a swagger request
func (o *PutLoggerParams) WriteToRequest(r runtime.ClientRequest, reg strfmt.Registry) error {

	if err := r.SetTimeout(o.timeout); err != nil {
		return err
	}
	var res []error
	if o.Body != nil {
		if err := r.SetBodyParam(o.Body); err != nil {
			return err
		}
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}
