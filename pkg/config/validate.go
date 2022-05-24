/**
 * Copyright 2022 Google LLC
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package config

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"hpc-toolkit/pkg/modulereader"
	"hpc-toolkit/pkg/sourcereader"
	"hpc-toolkit/pkg/validators"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

const (
	validationErrorMsg = "validation failed due to the issues listed above"
)

// validate is the top-level function for running the validation suite.
func (dc DeploymentConfig) validate() {
	if err := dc.validateVars(); err != nil {
		log.Fatal(err)
	}

	// variables should be validated before running validators
	if err := dc.executeValidators(); err != nil {
		log.Fatal(err)
	}

	if err := dc.validateModules(); err != nil {
		log.Fatal(err)
	}
	if err := dc.validateModuleSettings(); err != nil {
		log.Fatal(err)
	}
}

// performs validation of global variables
func (dc DeploymentConfig) executeValidators() error {
	var errored, warned bool
	implementedValidators := dc.getValidators()

	if dc.Config.ValidationLevel == validationIgnore {
		return nil
	}

	for _, validator := range dc.Config.Validators {
		if f, ok := implementedValidators[validator.Validator]; ok {
			err := f(validator)
			if err != nil {
				var prefix string
				switch dc.Config.ValidationLevel {
				case validationWarning:
					warned = true
					prefix = "warning: "
				default:
					errored = true
					prefix = "error: "
				}
				log.Print(prefix, err)
				log.Println()
			}
		} else {
			errored = true
			log.Printf("%s is not an implemented validator", validator.Validator)
		}
	}

	if warned || errored {
		log.Println("validator failures can indicate a credentials problem.")
		log.Println("troubleshooting info appears at:")
		log.Println()
		log.Println("https://github.com/GoogleCloudPlatform/hpc-toolkit/blob/main/README.md#supplying-cloud-credentials-to-terraform")
		log.Println()
		log.Println("validation can be configured:")
		log.Println("- treat failures as warnings by using the create command")
		log.Println("  with the flag \"--validation-level WARNING\"")
		log.Println("- can be disabled entirely by using the create command")
		log.Println("  with the flag \"--validation-level IGNORE\"")
		log.Println("- a custom set of validators can be configured following")
		log.Println("  instructions at:")
		log.Println()
		log.Println("https://github.com/GoogleCloudPlatform/hpc-toolkit/blob/main/README.md#blueprint-warnings-and-errors")
	}

	if errored {
		return fmt.Errorf(validationErrorMsg)
	}
	return nil
}

// validateVars checks the global variables for viable types
func (dc DeploymentConfig) validateVars() error {
	vars := dc.Config.Vars
	nilErr := "deployment variable %s was not set"

	// Check for project_id
	if _, ok := vars["project_id"]; !ok {
		log.Println("WARNING: No project_id in deployment variables")
	}

	// Check type of labels (if they are defined)
	if labels, ok := vars["labels"]; ok {
		if _, ok := labels.(map[string]interface{}); !ok {
			return errors.New("vars.labels must be a map")
		}
	}

	// Check for any nil values
	for key, val := range vars {
		if val == nil {
			return fmt.Errorf(nilErr, key)
		}
	}

	return nil
}

func module2String(c Module) string {
	cBytes, _ := yaml.Marshal(&c)
	return string(cBytes)
}

func validateModule(c Module) error {
	if c.ID == "" {
		return fmt.Errorf("%s\n%s", errorMessages["emptyID"], module2String(c))
	}
	if c.Source == "" {
		return fmt.Errorf("%s\n%s", errorMessages["emptySource"], module2String(c))
	}
	if !modulereader.IsValidKind(c.Kind) {
		return fmt.Errorf("%s\n%s", errorMessages["wrongKind"], module2String(c))
	}
	return nil
}

func hasIllegalChars(name string) bool {
	return !regexp.MustCompile(`^[\w\+]+(\s*)[\w-\+\.]+$`).MatchString(name)
}

func validateOutputs(mod Module, modInfo modulereader.ModuleInfo) error {

	// Only get the map if needed
	var outputsMap map[string]modulereader.VarInfo
	if len(mod.Outputs) > 0 {
		outputsMap = modInfo.GetOutputsAsMap()
	}

	// Ensure output exists in the underlying modules
	for _, output := range mod.Outputs {
		if _, ok := outputsMap[output]; !ok {
			return fmt.Errorf("%s, module: %s output: %s",
				errorMessages["invalidOutput"], mod.ID, output)
		}
	}
	return nil
}

// validateModules ensures parameters set in modules are set correctly.
func (dc DeploymentConfig) validateModules() error {
	for _, grp := range dc.Config.DeploymentGroups {
		for _, mod := range grp.Modules {
			if err := validateModule(mod); err != nil {
				return err
			}
			modInfo := dc.ModulesInfo[grp.Name][mod.Source]
			if err := validateOutputs(mod, modInfo); err != nil {
				return err
			}
		}
	}
	return nil
}

type moduleVariables struct {
	Inputs  map[string]bool
	Outputs map[string]bool
}

func validateSettings(
	mod Module,
	info modulereader.ModuleInfo) error {

	var cVars = moduleVariables{
		Inputs:  map[string]bool{},
		Outputs: map[string]bool{},
	}

	for _, input := range info.Inputs {
		cVars.Inputs[input.Name] = input.Required
	}
	// Make sure we only define variables that exist
	for k := range mod.Settings {
		if _, ok := cVars.Inputs[k]; !ok {
			return fmt.Errorf("%s: Module ID: %s Setting: %s",
				errorMessages["extraSetting"], mod.ID, k)
		}
	}
	return nil
}

// validateModuleSettings verifies that no additional settings are provided
// that don't have a counterpart variable in the module
func (dc DeploymentConfig) validateModuleSettings() error {
	for _, grp := range dc.Config.DeploymentGroups {
		for _, mod := range grp.Modules {
			reader := sourcereader.Factory(mod.Source)
			info, err := reader.GetModuleInfo(mod.Source, mod.Kind)
			if err != nil {
				errStr := "failed to get info for module at %s while validating module settings"
				return errors.Wrapf(err, errStr, mod.Source)
			}
			if err = validateSettings(mod, info); err != nil {
				errStr := "found an issue while validating settings for module at %s"
				return errors.Wrapf(err, errStr, mod.Source)
			}
		}
	}
	return nil
}

func (dc *DeploymentConfig) getValidators() map[string]func(validatorConfig) error {
	allValidators := map[string]func(validatorConfig) error{
		testProjectExistsName.String(): dc.testProjectExists,
		testRegionExistsName.String():  dc.testRegionExists,
		testZoneExistsName.String():    dc.testZoneExists,
		testZoneInRegionName.String():  dc.testZoneInRegion,
	}
	return allValidators
}

// check that the keys in inputs and requiredInputs are identical sets of strings
func testInputList(function string, inputs map[string]interface{}, requiredInputs []string) error {
	var errored bool
	for _, requiredInput := range requiredInputs {
		if _, found := inputs[requiredInput]; !found {
			log.Printf("a required input %s was not provided to %s!", requiredInput, function)
			errored = true
		}
	}

	if errored {
		return fmt.Errorf("at least one required input was not provided to %s", function)
	}

	// ensure that no extra inputs were provided by comparing length
	if len(requiredInputs) != len(inputs) {
		errStr := "only %v inputs %s should be provided to %s"
		return fmt.Errorf(errStr, len(requiredInputs), requiredInputs, function)
	}

	return nil
}

func (dc *DeploymentConfig) testProjectExists(validator validatorConfig) error {
	requiredInputs := []string{"project_id"}
	funcName := testProjectExistsName.String()
	funcErrorMsg := fmt.Sprintf("validator %s failed", funcName)

	if validator.Validator != funcName {
		return fmt.Errorf("passed wrong validator to %s implementation", funcName)
	}

	err := testInputList(validator.Validator, validator.Inputs, requiredInputs)
	if err != nil {
		log.Print(funcErrorMsg)
		return err
	}

	projectID, err := dc.getStringValue(validator.Inputs["project_id"])
	if err != nil {
		log.Print(funcErrorMsg)
		return err
	}

	// err is nil or an error
	err = validators.TestProjectExists(projectID)
	if err != nil {
		log.Print(funcErrorMsg)
	}
	return err
}

func (dc *DeploymentConfig) testRegionExists(validator validatorConfig) error {
	requiredInputs := []string{"project_id", "region"}
	funcName := testRegionExistsName.String()
	funcErrorMsg := fmt.Sprintf("validator %s failed", funcName)

	if validator.Validator != funcName {
		return fmt.Errorf("passed wrong validator to %s implementation", funcName)
	}

	err := testInputList(validator.Validator, validator.Inputs, requiredInputs)
	if err != nil {
		return err
	}

	projectID, err := dc.getStringValue(validator.Inputs["project_id"])
	if err != nil {
		log.Print(funcErrorMsg)
		return err
	}
	region, err := dc.getStringValue(validator.Inputs["region"])
	if err != nil {
		log.Print(funcErrorMsg)
		return err
	}

	// err is nil or an error
	err = validators.TestRegionExists(projectID, region)
	if err != nil {
		log.Print(funcErrorMsg)
	}
	return err
}

func (dc *DeploymentConfig) testZoneExists(validator validatorConfig) error {
	requiredInputs := []string{"project_id", "zone"}
	funcName := testZoneExistsName.String()
	funcErrorMsg := fmt.Sprintf("validator %s failed", funcName)

	if validator.Validator != funcName {
		return fmt.Errorf("passed wrong validator to %s implementation", funcName)
	}

	err := testInputList(validator.Validator, validator.Inputs, requiredInputs)
	if err != nil {
		return err
	}

	projectID, err := dc.getStringValue(validator.Inputs["project_id"])
	if err != nil {
		log.Print(funcErrorMsg)
		return err
	}
	zone, err := dc.getStringValue(validator.Inputs["zone"])
	if err != nil {
		log.Print(funcErrorMsg)
		return err
	}

	// err is nil or an error
	err = validators.TestZoneExists(projectID, zone)
	if err != nil {
		log.Print(funcErrorMsg)
	}
	return err
}

func (dc *DeploymentConfig) testZoneInRegion(validator validatorConfig) error {
	requiredInputs := []string{"project_id", "region", "zone"}
	funcName := testZoneInRegionName.String()
	funcErrorMsg := fmt.Sprintf("validator %s failed", funcName)

	if validator.Validator != funcName {
		return fmt.Errorf("passed wrong validator to %s implementation", funcName)
	}

	err := testInputList(validator.Validator, validator.Inputs, requiredInputs)
	if err != nil {
		return err
	}

	projectID, err := dc.getStringValue(validator.Inputs["project_id"])
	if err != nil {
		log.Print(funcErrorMsg)
		return err
	}
	zone, err := dc.getStringValue(validator.Inputs["zone"])
	if err != nil {
		log.Print(funcErrorMsg)
		return err
	}
	region, err := dc.getStringValue(validator.Inputs["region"])
	if err != nil {
		log.Print(funcErrorMsg)
		return err
	}

	// err is nil or an error
	err = validators.TestZoneInRegion(projectID, zone, region)
	if err != nil {
		log.Print(funcErrorMsg)
	}
	return err
}

// return the actual value of a global variable specified by the literal
// variable inputReference in form ((var.project_id))
// if it is a literal global variable defined as a string, return value as string
// in all other cases, return empty string and error
func (dc *DeploymentConfig) getStringValue(inputReference interface{}) (string, error) {
	varRef, ok := inputReference.(string)
	if !ok {
		return "", fmt.Errorf("the value %s cannot be cast to a string", inputReference)
	}

	if IsLiteralVariable(varRef) {
		varSlice := strings.Split(HandleLiteralVariable(varRef), ".")
		varSrc := varSlice[0]
		varName := varSlice[1]

		// because expand has already run, the global variable should have been
		// checked for existence. handle if user has explicitly passed
		// ((var.does_not_exit)) or ((not_a_varsrc.not_a_var))
		if varSrc == "var" {
			if val, ok := dc.Config.Vars[varName]; ok {
				valString, ok := val.(string)
				if ok {
					return valString, nil
				}
				return "", fmt.Errorf("the deployment variable %s is not a string", inputReference)
			}
		}
	}
	return "", fmt.Errorf("the value %s is not a deployment variable or was not defined", inputReference)
}
