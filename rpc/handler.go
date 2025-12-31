// Package rpc implements the JSON-RPC 2.0 handler for NovaCoin.
package rpc

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"unicode"
)

// Handler is the JSON-RPC request handler.
type Handler struct {
	// Registered API methods
	methods map[string]*methodInfo

	// API namespaces
	namespaces map[string]interface{}

	mu sync.RWMutex
}

// methodInfo holds metadata about a registered method.
type methodInfo struct {
	// The method function
	fn reflect.Value

	// Receiver object (if any)
	receiver reflect.Value

	// Number of arguments (excluding context)
	numArgs int

	// Whether the method accepts a context
	hasContext bool

	// Argument types
	argTypes []reflect.Type

	// Return type info
	hasError   bool
	resultType reflect.Type
}

// NewHandler creates a new JSON-RPC handler.
func NewHandler() *Handler {
	return &Handler{
		methods:    make(map[string]*methodInfo),
		namespaces: make(map[string]interface{}),
	}
}

// RegisterAPI registers an API namespace with its methods.
// The namespace name is used as prefix (e.g., "eth" -> "eth_getBalance").
func (h *Handler) RegisterAPI(namespace string, api interface{}) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.namespaces[namespace] = api

	// Use reflection to find all exported methods
	apiType := reflect.TypeOf(api)
	apiValue := reflect.ValueOf(api)

	for i := 0; i < apiType.NumMethod(); i++ {
		method := apiType.Method(i)

		// Skip unexported methods
		if !method.IsExported() {
			continue
		}

		// Convert method name to lowercase first letter
		methodName := toLowerFirst(method.Name)
		fullName := namespace + "_" + methodName

		// Analyze method signature
		info, err := analyzeMethod(method, apiValue)
		if err != nil {
			return fmt.Errorf("invalid method %s: %w", fullName, err)
		}

		h.methods[fullName] = info
	}

	return nil
}

// analyzeMethod analyzes a method and creates methodInfo.
func analyzeMethod(method reflect.Method, receiver reflect.Value) (*methodInfo, error) {
	methodType := method.Type

	info := &methodInfo{
		fn:       method.Func,
		receiver: receiver,
	}

	// Count arguments (skip receiver)
	numIn := methodType.NumIn() - 1 // Skip receiver
	argStart := 1                   // Skip receiver

	// Check for context as first argument
	if numIn > 0 {
		firstArg := methodType.In(1)
		if firstArg.Implements(reflect.TypeOf((*context.Context)(nil)).Elem()) {
			info.hasContext = true
			argStart = 2
			numIn--
		}
	}

	// Store argument types
	info.numArgs = numIn
	info.argTypes = make([]reflect.Type, numIn)
	for i := 0; i < numIn; i++ {
		info.argTypes[i] = methodType.In(argStart + i)
	}

	// Check return values
	numOut := methodType.NumOut()
	if numOut > 2 {
		return nil, fmt.Errorf("method can have at most 2 return values")
	}

	if numOut >= 1 {
		lastOut := methodType.Out(numOut - 1)
		if lastOut.Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			info.hasError = true
			if numOut == 2 {
				info.resultType = methodType.Out(0)
			}
		} else {
			info.resultType = methodType.Out(0)
		}
	}

	return info, nil
}

// Handle processes a JSON-RPC request and returns a response.
func (h *Handler) Handle(ctx context.Context, data []byte) []byte {
	// Check for empty request
	if len(data) == 0 {
		return h.errorResponse(nil, ErrParseError)
	}

	// Check for batch request
	if data[0] == '[' {
		return h.handleBatch(ctx, data)
	}

	// Single request
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return h.errorResponse(nil, ErrParseError)
	}

	resp := h.handleRequest(ctx, &req)
	result, _ := json.Marshal(resp)
	return result
}

// handleBatch processes a batch of JSON-RPC requests.
func (h *Handler) handleBatch(ctx context.Context, data []byte) []byte {
	var batch BatchRequest
	if err := json.Unmarshal(data, &batch); err != nil {
		return h.errorResponse(nil, ErrParseError)
	}

	if len(batch) == 0 {
		return h.errorResponse(nil, ErrInvalidRequest)
	}

	// Process each request
	responses := make(BatchResponse, 0, len(batch))
	for _, req := range batch {
		resp := h.handleRequest(ctx, &req)
		// Only include responses for requests with IDs (not notifications)
		if req.ID != nil {
			responses = append(responses, *resp)
		}
	}

	// Return empty array if all requests were notifications
	if len(responses) == 0 {
		return []byte("[]")
	}

	result, _ := json.Marshal(responses)
	return result
}

// handleRequest processes a single JSON-RPC request.
func (h *Handler) handleRequest(ctx context.Context, req *Request) *Response {
	// Validate request
	if req.JSONRPC != "2.0" {
		return &Response{
			JSONRPC: "2.0",
			Error:   ErrInvalidRequest,
			ID:      req.ID,
		}
	}

	// Find method
	h.mu.RLock()
	methodInfo, exists := h.methods[req.Method]
	h.mu.RUnlock()

	if !exists {
		return &Response{
			JSONRPC: "2.0",
			Error:   ErrMethodNotFound,
			ID:      req.ID,
		}
	}

	// Parse parameters
	args, err := h.parseParams(req.Params, methodInfo)
	if err != nil {
		return &Response{
			JSONRPC: "2.0",
			Error:   NewError(ErrCodeInvalidParams, err.Error()),
			ID:      req.ID,
		}
	}

	// Call method
	result, rpcErr := h.callMethod(ctx, methodInfo, args)
	if rpcErr != nil {
		return &Response{
			JSONRPC: "2.0",
			Error:   rpcErr,
			ID:      req.ID,
		}
	}

	return &Response{
		JSONRPC: "2.0",
		Result:  result,
		ID:      req.ID,
	}
}

// parseParams parses request parameters into method arguments.
func (h *Handler) parseParams(params json.RawMessage, info *methodInfo) ([]reflect.Value, error) {
	if info.numArgs == 0 {
		return nil, nil
	}

	// Try parsing as array first
	var rawParams []json.RawMessage
	if err := json.Unmarshal(params, &rawParams); err != nil {
		// Try parsing as object for single argument
		if info.numArgs == 1 {
			rawParams = []json.RawMessage{params}
		} else {
			return nil, fmt.Errorf("params must be an array")
		}
	}

	if len(rawParams) > info.numArgs {
		return nil, fmt.Errorf("too many arguments: got %d, want %d", len(rawParams), info.numArgs)
	}

	args := make([]reflect.Value, info.numArgs)
	for i := 0; i < info.numArgs; i++ {
		argType := info.argTypes[i]
		argValue := reflect.New(argType)

		if i < len(rawParams) && len(rawParams[i]) > 0 {
			if err := json.Unmarshal(rawParams[i], argValue.Interface()); err != nil {
				return nil, fmt.Errorf("invalid argument %d: %w", i, err)
			}
		} else {
			// Use zero value for missing optional arguments
			argValue.Elem().Set(reflect.Zero(argType))
		}

		args[i] = argValue.Elem()
	}

	return args, nil
}

// callMethod invokes the method with the given arguments.
func (h *Handler) callMethod(ctx context.Context, info *methodInfo, args []reflect.Value) (interface{}, *Error) {
	// Build full argument list
	fullArgs := make([]reflect.Value, 0, len(args)+2)
	fullArgs = append(fullArgs, info.receiver)

	if info.hasContext {
		fullArgs = append(fullArgs, reflect.ValueOf(ctx))
	}

	fullArgs = append(fullArgs, args...)

	// Recover from panics
	defer func() {
		if r := recover(); r != nil {
			// Will be handled by caller
		}
	}()

	// Call the method
	results := info.fn.Call(fullArgs)

	// Handle return values
	if info.hasError {
		errIdx := len(results) - 1
		if !results[errIdx].IsNil() {
			err := results[errIdx].Interface().(error)
			// Check if it's already an RPC error
			if rpcErr, ok := err.(*Error); ok {
				return nil, rpcErr
			}
			return nil, NewError(ErrCodeInternal, err.Error())
		}

		if info.resultType != nil {
			return results[0].Interface(), nil
		}
		return nil, nil
	}

	if info.resultType != nil {
		return results[0].Interface(), nil
	}

	return nil, nil
}

// errorResponse creates an error response.
func (h *Handler) errorResponse(id interface{}, err *Error) []byte {
	resp := Response{
		JSONRPC: "2.0",
		Error:   err,
		ID:      id,
	}
	result, _ := json.Marshal(resp)
	return result
}

// GetMethods returns all registered method names.
func (h *Handler) GetMethods() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	methods := make([]string, 0, len(h.methods))
	for name := range h.methods {
		methods = append(methods, name)
	}
	return methods
}

// GetNamespaces returns all registered namespaces.
func (h *Handler) GetNamespaces() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	namespaces := make([]string, 0, len(h.namespaces))
	for name := range h.namespaces {
		namespaces = append(namespaces, name)
	}
	return namespaces
}

// HasMethod checks if a method is registered.
func (h *Handler) HasMethod(name string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, exists := h.methods[name]
	return exists
}

// === Helper Functions ===

// toLowerFirst converts the first character to lowercase.
func toLowerFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToLower(runes[0])
	return string(runes)
}

// ParseMethodNamespace splits a method name into namespace and method.
func ParseMethodNamespace(fullName string) (namespace, method string) {
	parts := strings.SplitN(fullName, "_", 2)
	if len(parts) != 2 {
		return "", fullName
	}
	return parts[0], parts[1]
}
