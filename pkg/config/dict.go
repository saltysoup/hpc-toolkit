// Copyright 2023 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"encoding/json"
	"fmt"

	"github.com/zclconf/go-cty/cty"
	"github.com/zclconf/go-cty/cty/gocty"
	ctyJson "github.com/zclconf/go-cty/cty/json"
	"gopkg.in/yaml.v3"
)

// Dict maps string key to cty.Value.
// Zero Dict value is initialized (as oposed to nil map).
type Dict struct {
	m map[string]cty.Value
}

// Get returns stored value or cty.NilVal.
func (d *Dict) Get(k string) cty.Value {
	if d.m == nil {
		return cty.NilVal
	}
	return d.m[k]
}

// Has tests if key is present in map.
func (d *Dict) Has(k string) bool {
	if d.m == nil {
		return false
	}
	_, ok := d.m[k]
	return ok
}

// Set adds/overrides value by key.
// Returns reference to Dict-self.
func (d *Dict) Set(k string, v cty.Value) *Dict {
	if d.m == nil {
		d.m = map[string]cty.Value{k: v}
	} else {
		d.m[k] = v
	}
	return d
}

// Items returns instance of map[string]cty.Value
// will same set of key-value pairs as stored in Dict.
// This map is a copy, changes to returned map have no effect on the Dict.
func (d *Dict) Items() map[string]cty.Value {
	m := map[string]cty.Value{}
	if d.m != nil {
		for k, v := range d.m {
			m[k] = v
		}
	}
	return m
}

// AsObject returns Dict as cty.ObjectVal
func (d *Dict) AsObject() cty.Value {
	return cty.ObjectVal(d.Items())
}

// yamlValue is wrapper around cty.Value to handle YAML unmarshal.
type yamlValue struct {
	v cty.Value
}

// UnmarshalYAML implements custom YAML unmarshaling.
func (y *yamlValue) UnmarshalYAML(n *yaml.Node) error {
	var err error
	switch n.Kind {
	case yaml.ScalarNode:
		err = y.unmarshalScalar(n)
	case yaml.MappingNode:
		err = y.unmarshalObject(n)
	case yaml.SequenceNode:
		err = y.unmarshalTuple(n)
	default:
		err = fmt.Errorf("line %d: cannot decode node with unknown kind %d", n.Line, n.Kind)
	}
	return err
}

func (y *yamlValue) unmarshalScalar(n *yaml.Node) error {
	var s interface{}
	if err := n.Decode(&s); err != nil {
		return err
	}
	ty, err := gocty.ImpliedType(s)
	if err != nil {
		return err
	}
	y.v, err = gocty.ToCtyValue(s, ty)
	return err
}

func (y *yamlValue) unmarshalObject(n *yaml.Node) error {
	var my map[string]yamlValue
	if err := n.Decode(&my); err != nil {
		return err
	}
	mv := map[string]cty.Value{}
	for k, y := range my {
		mv[k] = y.v
	}
	y.v = cty.ObjectVal(mv)
	return nil
}

func (y *yamlValue) unmarshalTuple(n *yaml.Node) error {
	var ly []yamlValue
	if err := n.Decode(&ly); err != nil {
		return err
	}
	lv := []cty.Value{}
	for _, y := range ly {
		lv = append(lv, y.v)
	}
	y.v = cty.TupleVal(lv)
	return nil
}

// UnmarshalYAML implements custom YAML unmarshaling.
func (d *Dict) UnmarshalYAML(n *yaml.Node) error {
	var m map[string]yamlValue
	if err := n.Decode(&m); err != nil {
		return err
	}
	for k, y := range m {
		d.Set(k, y.v)
	}
	return nil
}

// MarshalYAML implements custom YAML marshaling.
func (d Dict) MarshalYAML() (interface{}, error) {
	mi := map[string]interface{}{}
	for k, v := range d.Items() {
		j := ctyJson.SimpleJSONValue{Value: v}
		b, err := j.MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("failed to marshal JSON: %v", err)
		}
		var g interface{}
		err = json.Unmarshal(b, &g)
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal JSON: %v", err)
		}
		mi[k] = g
	}
	return mi, nil
}