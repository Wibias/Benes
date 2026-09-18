package openairesponses

import (
	"fmt"

	"github.com/Wibias/Benes/internal/protocol"
	"github.com/Wibias/Benes/internal/providers"
	"github.com/Wibias/Benes/internal/tools"
)

type toolIdentityStream struct {
	inner   providers.EventStream
	byWire  map[string]protocol.Tool
	bindErr error
}

func wrapToolIdentityStream(stream providers.EventStream, request protocol.ParsedRequest) providers.EventStream {
	if stream == nil {
		return nil
	}
	byWire, err := materializedToolIdentities(request)
	if err == nil && len(byWire) == 0 {
		return stream
	}
	return &toolIdentityStream{inner: stream, byWire: byWire, bindErr: err}
}

func materializedToolIdentities(request protocol.ParsedRequest) (map[string]protocol.Tool, error) {
	functionTools := make([]protocol.Tool, 0, len(request.Context.Tools))
	for _, tool := range request.Context.Tools {
		if tool.HostedWebSearch {
			continue
		}
		functionTools = append(functionTools, tool)
	}
	catalog, err := tools.Build(functionTools)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedRequestShape, err)
	}
	plan, err := catalog.Materialize(tools.MaterializeOptions{Choice: request.Options.ToolChoice, Messages: request.Context.Messages})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedRequestShape, err)
	}
	catalog, err = tools.Build(plan.Tools)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnsupportedRequestShape, err)
	}
	byWire := make(map[string]protocol.Tool, len(catalog.Entries()))
	for _, entry := range catalog.Entries() {
		byWire[entry.WireName] = entry.Tool
	}
	return byWire, nil
}

func (s *toolIdentityStream) Next() (protocol.Event, error) {
	if s.bindErr != nil {
		return protocol.Event{}, s.bindErr
	}
	event, err := s.inner.Next()
	if err != nil {
		return protocol.Event{}, err
	}
	if event.Type != protocol.EventToolCallStart {
		return event, nil
	}
	tool, ok := s.byWire[event.Name]
	if !ok {
		return event, nil
	}
	event.Name = tool.Name
	event.Namespace = tool.Namespace
	return event, nil
}

func (s *toolIdentityStream) Close() error {
	if s.inner == nil {
		return nil
	}
	return s.inner.Close()
}

func (s *toolIdentityStream) PhysicalOwnership() *providers.PhysicalPin {
	if s == nil || s.inner == nil {
		return nil
	}
	if reporter, ok := s.inner.(interface{ PhysicalOwnership() *providers.PhysicalPin }); ok {
		return reporter.PhysicalOwnership()
	}
	return nil
}
