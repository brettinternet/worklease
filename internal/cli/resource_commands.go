package cli

import (
	"context"
	"strings"

	"github.com/brettinternet/worklease/internal/output"
	"github.com/brettinternet/worklease/internal/reason"
	"github.com/brettinternet/worklease/internal/resource"
	urfave "github.com/urfave/cli/v3"
)

func writeResourceResult(s *boundary, cmd *urfave.Command, operation string, fields map[string]any) error {
	if s.jsonRequested(cmd) {
		return output.WriteSuccess(s.writer, operation, fields)
	}
	return output.WriteText(s.writer, operation, fields)
}
func keyFields(key resource.Key) map[string]any {
	fields := map[string]any{"provider": key.Provider, "resource": key.Resource, "capability": key.Capability, "scope": key.Scope, "identityScope": key.IdentityScope, "localReplaceAllowed": key.LocalReplaceAllowed, "providerFencing": false, "genericExecutionGuarantee": "local-coordination"}
	if key.Source != "" {
		fields["source"] = key.Source
	}
	if key.Item != "" {
		fields["item"] = key.Item
	}
	return fields
}
func (s *boundary) keyResult(cmd *urfave.Command) error {
	in, err := ResolveResourceInput(cmd)
	if err != nil {
		return s.handle(cmd, err)
	}
	if len(in.Keys) != 1 {
		return s.handle(cmd, reason.New(reason.ReasonInvalidResource, "key requires exactly one resource"))
	}
	return writeResourceResult(s, cmd, "key", keyFields(in.Keys[0]))
}
func descriptorFields(d resource.Descriptor) map[string]any {
	m := d.Map()
	m["provider"] = d.Name
	m["providerFencing"] = false
	m["genericExecutionGuarantee"] = "local-coordination"
	return m
}
func (s *boundary) policyListResult(cmd *urfave.Command) error {
	values := resource.Descriptors()
	if s.jsonRequested(cmd) {
		out := make([]any, 0, len(values))
		for _, d := range values {
			out = append(out, descriptorFields(d))
		}
		return output.WriteSuccess(s.writer, "policy list", map[string]any{"policies": out})
	}
	return writePolicyListText(s.writer, values, cmd.Bool("full"), output.ColorEnabled(s.writer))
}
func (s *boundary) policyDescribeResult(cmd *urfave.Command) error {
	name := strings.TrimSpace(cmd.String("name"))
	if name == "" && cmd.Args().Len() > 0 {
		name = cmd.Args().First()
	}
	p, err := resource.Lookup(name)
	if err != nil {
		return s.handle(cmd, err)
	}
	return writeResourceResult(s, cmd, "policy describe", descriptorFields(p.Describe()))
}
func keyAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(_ context.Context, cmd *urfave.Command) error { return s.keyResult(cmd) }
}
func policyListAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(_ context.Context, cmd *urfave.Command) error { return s.policyListResult(cmd) }
}
func policyDescribeAction(s *boundary) func(context.Context, *urfave.Command) error {
	return func(_ context.Context, cmd *urfave.Command) error { return s.policyDescribeResult(cmd) }
}
