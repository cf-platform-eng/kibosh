// kibosh
//
// Copyright (c) 2017-Present Pivotal Software, Inc. All Rights Reserved.
//
// This program and the accompanying materials are made available under the terms of the under the Apache License,
// Version 2.0 (the "License”); you may not use this file except in compliance with the License. You may
// obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software distributed under the
// License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing permissions and
// limitations under the License.

package helm

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ghodss/yaml"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"k8s.io/client-go/tools/clientcmd"
	k8sAPI "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/proto/hapi/chart"
)

type MyChart struct {
	chart.Chart

	PrivateRegistryServer string          `json:"privateRegistryServer"`
	TransformedValues     []byte          `json:"transformedValues"`
	BindTemplate          string          `json:"bindTemplate"`
	Plans                 map[string]Plan `json:"plans"`
	ChartPath             string          `json:"chartPath"`
}

type Bind struct {
	Template string `json:"template"`
}

func NewChartValidationError(err error) *ChartValidationError {
	return &ChartValidationError{
		error: err,
	}
}

type ChartValidationError struct {
	error
}

type Plan struct {
	Name            string   `json:"name" json:"name"`
	Description     string   `json:"description" json:"description"`
	Bullets         []string `json:"bullets" json:"bullets"`
	File            string   `json:"file" json:"file"`
	Free            *bool    `json:"free,omitempty" json:"free"`
	Bindable        *bool    `json:"bindable,omitempty" json:"bindable"`
	CredentialsPath string   `json:"credentials" json:"credentialsPath"`

	Values        []byte         `json:"values"`
	ClusterConfig *k8sAPI.Config `json:"clusterConfig"`
}

func LoadFromDir(dir string, log *logrus.Logger) ([]*MyChart, error) {
	sourceDirStat, err := os.Stat(dir)
	if err != nil {
		return nil, err
	}
	if !sourceDirStat.IsDir() {
		return nil, errors.New(fmt.Sprintf("The provided path [%s] is not a directory", dir))
	}
	sources, err := ioutil.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	charts := []*MyChart{}
	for _, source := range sources {
		chartPath := path.Join(dir, source.Name())
		c, err := NewChart(chartPath, "", log)
		if err != nil {
			log.Debug(fmt.Sprintf("The file [%s] not failed to load as a chart", chartPath), err)
		} else {
			charts = append(charts, c)
		}
	}

	return charts, nil
}

func NewChart(chartPath string, privateRegistryServer string, log *logrus.Logger) (*MyChart, error) {
	myChart := &MyChart{
		PrivateRegistryServer: privateRegistryServer,
	}

	chartPathStat, err := os.Stat(chartPath)
	if err != nil {
		return nil, NewChartValidationError(err)
	}

	if chartPathStat.IsDir() {
		err = myChart.ensureIgnore(chartPath)
		if err != nil {
			return nil, errors.Wrap(err, "Error fixing .helmignore")
		}
	}

	loadedChart, err := chartutil.Load(chartPath)
	if err != nil {
		return nil, NewChartValidationError(err)
	}
	myChart.Chart = *loadedChart

	err = myChart.LoadChartValues()
	if err != nil {
		return nil, NewChartValidationError(err)
	}

	if chartPathStat.IsDir() {
		err = myChart.loadOSBAPIMetadataFromDirectory(chartPath, log)
	} else {
		err = myChart.loadOSBAPIMetadataFromArchive(chartPath, log)
	}
	if err != nil {
		return nil, NewChartValidationError(err)
	}

	if len(myChart.Plans) < 1 {
		defaultPlan := Plan{
			Name:        "default",
			Description: "Plan with default values",
		}
		myChart.SetPlanDefaultValues(&defaultPlan)
		myChart.Plans = map[string]Plan{
			"default": defaultPlan,
		}
	}

	myChart.ChartPath = chartPath
	return myChart, nil
}

func (c *MyChart) LoadChartValues() error {
	baseVals := map[string]interface{}{}
	if c.Chart.Values == nil {
		return errors.New("values.yaml is requires")
	}
	err := yaml.Unmarshal([]byte(c.Chart.Values.Raw), &baseVals)
	if err != nil {
		return err
	}

	transformed, err := c.OverrideImageSources(baseVals)
	if err != nil {
		return err
	}

	finalVals, err := yaml.Marshal(transformed)
	if err != nil {
		return err
	}

	c.TransformedValues = finalVals

	return nil
}

func (c *MyChart) OverrideImageSources(rawVals map[string]interface{}) (map[string]interface{}, error) {
	if c.PrivateRegistryServer == "" {
		return rawVals, nil
	}

	transformedVals := map[string]interface{}{}
	for key, val := range rawVals {
		if key == "image" {
			stringVal, ok := val.(string)
			if !ok {
				return nil, errors.New("'image' key value is not a string, vals structure is incorrect")
			}
			split := strings.Split(stringVal, "/")
			transformedVals[key] = fmt.Sprintf("%s/%s", c.PrivateRegistryServer, split[len(split)-1])
		} else if key == "images" {
			remarshalled, err := yaml.Marshal(val)
			if err != nil {
				return nil, err
			}

			imageMap := map[string]map[string]interface{}{}
			err = yaml.Unmarshal(remarshalled, &imageMap)
			if err != nil {
				return nil, err
			}

			for imageName, imageDefMap := range imageMap {
				transformedImage, err := c.OverrideImageSources(imageDefMap)
				if err != nil {
					return nil, err
				}
				imageMap[imageName] = transformedImage
			}
			transformedVals["images"] = imageMap
		} else if key == "global" {
			remarshalled, err := yaml.Marshal(val)
			if err != nil {
				return nil, err
			}

			globalMap := map[string]interface{}{}
			err = yaml.Unmarshal(remarshalled, &globalMap)
			if err != nil {
				return nil, err
			}

			if globalVal, ok := globalMap["imageRegistry"]; ok {
				stringVal, ok := globalVal.(string)
				if !ok {
					return nil, errors.New("'imageRegistry' key value is not a string, vals structure is incorrect")
				}
				split := strings.Split(stringVal, "/")
				globalMap["imageRegistry"] = fmt.Sprintf("%s/%s", c.PrivateRegistryServer, split[len(split)-1])
				transformedVals["global"] = globalMap
			} else {
				transformedVals[key] = val
			}
		} else {
			transformedVals[key] = val
		}
	}
	return transformedVals, nil
}

func (c *MyChart) loadOSBAPIMetadataFromArchive(chartPath string, log *logrus.Logger) error {
	chartFile, err := os.Open(chartPath)
	if err != nil {
		return err
	}
	defer chartFile.Close()

	gzipReader, err := gzip.NewReader(chartFile)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)

	tempDir, err := ioutil.TempDir("", c.Metadata.Name+"-plans")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)

	plans := []Plan{}
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}

		if strings.HasSuffix(header.Name, "plans.yaml") || strings.HasSuffix(header.Name, "plans.yml") {
			log.Info(fmt.Sprintf("plans.yaml found, reading from plans.yaml"))
			plansBytes, err := ioutil.ReadAll(tarReader)
			if err != nil {
				return err
			}

			err = yaml.Unmarshal(plansBytes, &plans)
			if err != nil {
				log.Info(fmt.Sprintf("Error unmarshalling plan"))
				return err
			}
		} else if strings.Contains(header.Name, "/plans") {
			filePath := path.Join(tempDir, header.Name[strings.Index(header.Name, "plans/")+6:])
			dst, err := os.Create(filePath)
			if err != nil {
				return err
			}

			_, err = io.Copy(dst, tarReader)
			if err != nil {
				return err
			}
		}

		if strings.HasSuffix(header.Name, "bind.yaml") || strings.HasSuffix(header.Name, "bind.yml") {
			dst := &bytes.Buffer{}
			_, err = io.Copy(dst, tarReader)
			if err != nil {
				return err
			}

			bind := &Bind{}
			err = yaml.Unmarshal(dst.Bytes(), bind)
			if err != nil {
				return err
			}

			c.BindTemplate = bind.Template
		}
	}

	err = c.loadPlans(tempDir, plans)

	return err
}

func (c *MyChart) loadOSBAPIMetadataFromDirectory(chartPath string, log *logrus.Logger) error {
	bindTemplatePath := path.Join(chartPath, "bind.yaml")
	_, err := os.Stat(bindTemplatePath)
	if err != nil {
		bindTemplatePath = path.Join(chartPath, "bind.yml")
		_, err := os.Stat(bindTemplatePath)
		if err != nil {
			bindTemplatePath = ""
		}
	}

	if bindTemplatePath != "" {
		bindTemplateBytes, err := ioutil.ReadFile(bindTemplatePath)
		if err != nil {
			return err
		}

		bind := &Bind{}
		err = yaml.Unmarshal(bindTemplateBytes, bind)
		if err != nil {
			return err
		}

		c.BindTemplate = bind.Template
	}

	plansPath := path.Join(chartPath, "plans.yaml")
	_, err = os.Stat(plansPath)
	if err != nil {
		plansPath = path.Join(chartPath, "plans.yml")
		_, err := os.Stat(plansPath)
		if err != nil {
			_, ok := err.(*os.PathError)
			if ok {
				log.Info(fmt.Sprintf("No plan file found in path %s, creating default plan", chartPath))
				c.Plans = map[string]Plan{}
				return nil
			} else {
				return err
			}
		}
	}

	plansBytes, err := ioutil.ReadFile(plansPath)
	if err != nil {
		return err
	}

	plans := []Plan{}
	err = yaml.Unmarshal(plansBytes, &plans)
	if err != nil {
		return err
	}

	return c.loadPlans(filepath.Join(chartPath, "plans"), plans)
}

func (c *MyChart) loadPlans(plansPath string, plans []Plan) error {
	c.Plans = map[string]Plan{}

	for _, p := range plans {
		planValues, err := ioutil.ReadFile(filepath.Join(plansPath, p.File))
		if err != nil {
			return err
		}
		p.Values = planValues

		c.SetPlanDefaultValues(&p)
		match, err := regexp.MatchString(`^[0-9a-z.\-]+$`, p.Name)
		if err != nil {
			return err
		}
		if !match {
			return errors.New(fmt.Sprintf("Name [%s] contains invalid characters", p.Name))
		}

		if p.CredentialsPath != "" {
			loader := &clientcmd.ClientConfigLoadingRules{
				ExplicitPath: filepath.Join(plansPath, p.CredentialsPath),
			}
			loadedConfig, err := loader.Load()
			if err != nil {
				return err
			}

			p.ClusterConfig = loadedConfig
		}

		c.Plans[p.Name] = p
	}

	return nil
}

func (c *MyChart) SetPlanDefaultValues(plan *Plan) {
	if plan.Free == nil {
		t := true
		plan.Free = &t
	}
	if plan.Bindable == nil {
		t := true
		plan.Bindable = &t
	}
}

func (c *MyChart) ensureIgnore(chartPath string) error {
	_, err := os.Stat(chartPath)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("Error reading chart dir [%s]", chartPath))
	}

	ignoreFilePath := filepath.Join(chartPath, ".helmignore")
	_, err = os.Stat(ignoreFilePath)
	if err != nil {
		file, err := os.Create(ignoreFilePath)
		defer file.Close()
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("Error creating .helmignore [%s]", chartPath))
		} else {
			file.Write([]byte("images"))
		}
	} else {
		contents, err := ioutil.ReadFile(ignoreFilePath)
		lines := strings.Split(string(contents), "\n")
		for _, line := range lines {
			if line == "images" {
				return nil
			}
		}

		file, err := os.OpenFile(ignoreFilePath, os.O_APPEND|os.O_WRONLY, 0666)
		defer file.Close()
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("Error opening .helmignore [%s]", chartPath))
		} else {
			_, err = file.Write([]byte("\nimages\n"))
			if err != nil {
				return errors.Wrap(err, fmt.Sprintf("Error appending to .helmignore [%s]", chartPath))
			}
		}
	}

	return nil
}

func (c *MyChart) String() string {
	return c.Metadata.Name
}
