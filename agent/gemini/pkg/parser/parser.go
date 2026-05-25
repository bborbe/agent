// Copyright (c) 2026 Benjamin Borbe All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package parser provides a Gemini-backed implementation of agentlib.AIParser.
//
// Boundary translator: takes free-form markdown and populates a typed Go
// struct via Gemini structured-output (ResponseMIMEType: application/json
// + ResponseSchema derived reflectively from the target type).
//
// Ported from trading/agent/backtest/pkg/task-content-parser.go (the
// pioneering implementation). Kept agent-local until a 2nd consumer
// emerges (Rule of Three) — promote to lib/gemini/ when that happens.
package parser

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/bborbe/errors"
	"github.com/golang/glog"
	"google.golang.org/genai"

	libdelivery "github.com/bborbe/agent/lib/delivery"
)

// Parser implements agentlib.AIParser via the Gemini API.
type Parser struct {
	client *genai.Client
	model  string
}

// New constructs a Parser that calls the Gemini API directly.
func New(ctx context.Context, apiKey string, model string) (*Parser, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, errors.Wrap(
			ctx,
			err,
			"create genai client failed",
		)
	}
	return NewWithClient(client, model), nil
}

// NewWithClient constructs a Parser using an existing genai client (for tests).
func NewWithClient(client *genai.Client, model string) *Parser {
	return &Parser{client: client, model: model}
}

// Parse implements agentlib.AIParser. Reads free-form taskContent and
// populates the target struct via Gemini structured-output.
func (p *Parser) Parse(ctx context.Context, taskContent string, target any) error {
	if strings.TrimSpace(taskContent) == "" {
		return errors.Errorf(ctx, "task content is empty")
	}

	schema, err := buildGenAISchema(ctx, target)
	if err != nil {
		return errors.Wrap(ctx, err, "build schema for target")
	}

	config := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema:   schema,
	}

	glog.V(3).Infof("gemini parse: model=%s content-length=%d", p.model, len(taskContent))
	resp, err := p.client.Models.GenerateContent(ctx, p.model, genai.Text(taskContent), config)
	if err != nil {
		return errors.Wrap(ctx, err, "generate content failed")
	}

	text := libdelivery.StripMarkdownCodeFences(resp.Text())
	glog.V(3).Infof("gemini parse: raw response=%q", text)

	if err := json.Unmarshal([]byte(text), target); err != nil {
		glog.Warningf("gemini parse: unmarshal failed: response=%q err=%v", text, err)
		return errors.Wrap(ctx, err, "unmarshal response failed")
	}

	return nil
}

const maxSchemaDepth = 8

// buildGenAISchema derives a *genai.Schema from the concrete type of target
// (a pointer to a struct).
func buildGenAISchema(ctx context.Context, target any) (*genai.Schema, error) {
	t := reflect.TypeOf(target)
	if t == nil {
		return &genai.Schema{Type: genai.TypeObject}, nil
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return buildSchemaForTypeAtDepth(ctx, t, 0)
}

func buildSchemaForTypeAtDepth(
	ctx context.Context,
	t reflect.Type,
	depth int,
) (*genai.Schema, error) {
	if depth > maxSchemaDepth {
		return nil, errors.Errorf(
			ctx,
			"schema derivation exceeded max depth %d — possible recursive struct",
			maxSchemaDepth,
		)
	}
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.String:
		return &genai.Schema{Type: genai.TypeString}, nil
	case reflect.Bool:
		return &genai.Schema{Type: genai.TypeBoolean}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return &genai.Schema{Type: genai.TypeInteger}, nil
	case reflect.Float32, reflect.Float64:
		return &genai.Schema{Type: genai.TypeNumber}, nil
	case reflect.Map:
		return &genai.Schema{Type: genai.TypeObject}, nil
	case reflect.Slice:
		itemSchema, err := buildSchemaForTypeAtDepth(ctx, t.Elem(), depth+1)
		if err != nil {
			return nil, errors.Wrap(ctx, err, "slice element")
		}
		return &genai.Schema{Type: genai.TypeArray, Items: itemSchema}, nil
	case reflect.Struct:
		return buildStructSchemaAtDepth(ctx, t, depth)
	default:
		return nil, errors.Errorf(ctx, "unsupported kind %s in schema derivation", t.Kind())
	}
}

func buildStructSchemaAtDepth(
	ctx context.Context,
	t reflect.Type,
	depth int,
) (*genai.Schema, error) {
	props := make(map[string]*genai.Schema)
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		jsonTag := field.Tag.Get("json")
		name := field.Name
		if jsonTag != "" {
			parts := strings.SplitN(jsonTag, ",", 2)
			if parts[0] == "-" {
				continue
			}
			if parts[0] != "" {
				name = parts[0]
			}
		}
		fieldSchema, err := buildSchemaForTypeAtDepth(ctx, field.Type, depth+1)
		if err != nil {
			return nil, errors.Wrapf(ctx, err, "field %s", field.Name)
		}
		props[name] = fieldSchema
	}
	return &genai.Schema{
		Type:       genai.TypeObject,
		Properties: props,
	}, nil
}
