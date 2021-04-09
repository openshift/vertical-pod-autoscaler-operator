package main

import (
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/util/json"

	corev1 "k8s.io/api/core/v1"
	apimachineryyaml "k8s.io/apimachinery/pkg/util/yaml"
)

const (
	ImageFormatVar       = "IMAGE_FORMAT"
	ComponentPlaceholder = "${component}"

	// The matching component names in CI for the operator, operand and the operator registry image.
	OperatorName = "vertical-pod-autoscaler-operator"
	OperandName  = "vertical-pod-autoscaler"
	RegistryName = "vpa-operator-registry"
)

// all keys in data section of the ConfigMap
// each key will be injected into the operator registry pod's ENV
const (
	// The init container needs to know the location of the CSV file (inside the operator registry image)
	// which we want to mutate to inject the right operator and operand image built by CI.
	csvFilePathEnv = "CSV_FILE_PATH_IN_REGISTRY_IMAGE"

	// This points to the current operator image URL in te CSV file that we want to replace with
	// the one built by CI.
	oldOperatorImageEnv = "OLD_OPERATOR_IMAGE_URL_IN_CSV"

	// This points to the current operand image URL in te CSV file that we want to replace with
	// the one built by CI.
	oldOperandImageEnv = "OLD_OPERAND_IMAGE_URL_IN_CSV"

	// This points to the operator image built by CI that we want to inject into the CSV.
	operatorImageEnv = "OPERATOR_IMAGE_URL"

	// This points to the operand image built by CI that we want to inject into the CSV.
	operandImageEnv = "OPERAND_IMAGE_URL"

	// This points to the operator registry image built by CI that we are
	// going to deploy for e2e testing.
	operatorRegistryImageEnv = "OPERATOR_REGISTRY_IMAGE_URL"
)

// env vars for local e2e testing
// We want to local e2e testing to have the same approach for consistency.
// The following env vars are needed to point to the operator, operand and the registry image.
const (
	localOperatorImageEnv = "LOCAL_OPERATOR_IMAGE"
	localOperandImageEnv  = "LOCAL_OPERAND_IMAGE"
	localRegistryImageEnv = "LOCAL_OPERATOR_REGISTRY_IMAGE"
)

var (
	mode      = flag.String("mode", "", "operation mode, either local or ci")
	isOLM     = flag.Bool("olm", true, "indicates whether we are deploying the operator using OLM")
	configmap = flag.String("configmap", "", "path to the ConfigMap file")
)

type Image struct {
	Format   string
	Registry string
	Operator string
	Operand  string
}

type ImageURLGetter func() (image *Image, err error)
type Loader func(object *corev1.ConfigMap) error

func main() {
	flag.Parse()

	if *configmap == "" {
		fmt.Fprintf(os.Stderr, "ConfigMap file name can not be empty")
		os.Exit(1)
	}

	work := func(loader Loader) error {
		cm, err := read(*configmap)
		if err != nil {
			return err
		}

		if err = loader(cm); err != nil {
			return err
		}

		return write(*configmap, cm)
	}

	l := func(mode string, isOLM bool) (loader Loader, err error) {
		switch {
		case mode == "ci" && isOLM:
			loader = withOLMWithCI
		case mode == "ci":
			loader = withCI
		case mode == "local" && isOLM:
			loader = withOLMWithLocal
		case mode == "local":
			loader = withLocal
		default:
			err = fmt.Errorf("unsupported mode, value=%s", mode)
		}

		return
	}

	loader, err := l(*mode, *isOLM)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ran in to error - %s", err.Error())
		os.Exit(1)
	}

	if err = work(loader); err != nil {
		fmt.Fprintf(os.Stderr, "ran in to error - %s", err.Error())
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "successfully updated ConfigMap env file=%s", *configmap)
	os.Exit(0)
}

func withOLMWithLocal(object *corev1.ConfigMap) error {
	if err := populate(getLocalImageURL, object); err != nil {
		return err
	}

	return validateWithOLM(object)
}

func withOLMWithCI(object *corev1.ConfigMap) error {
	if err := populate(getCIImageURL, object); err != nil {
		return err
	}

	return validateWithOLM(object)
}

func withLocal(object *corev1.ConfigMap) error {
	if err := populate(getLocalImageURL, object); err != nil {
		return err
	}

	return nil
}

func withCI(object *corev1.ConfigMap) error {
	if err := populate(getCIImageURL, object); err != nil {
		return err
	}

	return nil
}

func validateWithOLM(object *corev1.ConfigMap) error {
	checkEnv := func(obj *corev1.ConfigMap, key string) error {
		if value, ok := object.Data[key]; ok {
			if value == "" {
				return fmt.Errorf("ENV var %s not defined", key)
			} else {
				return nil
			}
		} else {
			return fmt.Errorf("ENV var %s not defined", key)
		}
	}

	if err := checkEnv(object, oldOperandImageEnv); err != nil {
		return err
	}

	if err := checkEnv(object, csvFilePathEnv); err != nil {
		return err
	}

	if err := checkEnv(object, operatorImageEnv); err != nil {
		return err
	}

	if err := checkEnv(object, operandImageEnv); err != nil {
		return err
	}

	if err := checkEnv(object, operatorRegistryImageEnv); err != nil {
		return err
	}

	return nil
}

// This function populates the ConfigMap object's data section with all the ENV variables
// so that we can propagate these ENV variables to the init container.
func populate(getter ImageURLGetter, object *corev1.ConfigMap) error {
	image, err := getter()
	if err != nil {
		return err
	}

	oldOperatorImageURL := os.Getenv(oldOperatorImageEnv)
	oldOperandImageURL := os.Getenv(oldOperandImageEnv)
	csvFilePath := os.Getenv(csvFilePathEnv)

	if len(object.Data) == 0 {
		object.Data = map[string]string{}
	}

	object.Data[csvFilePathEnv] = csvFilePath
	object.Data[operatorRegistryImageEnv] = image.Registry
	object.Data[oldOperatorImageEnv] = oldOperatorImageURL
	object.Data[operatorImageEnv] = image.Operator

	object.Data[oldOperandImageEnv] = oldOperandImageURL
	object.Data[operandImageEnv] = image.Operand

	return nil
}

// This function retrieves the URL of the operator image, operand image and the operator registry image
// that are built locally.
func getLocalImageURL() (image *Image, err error) {
	registry := os.Getenv(localRegistryImageEnv)
	operator := os.Getenv(localOperatorImageEnv)
	operand := os.Getenv(localOperandImageEnv)

	image = &Image{
		Registry: registry,
		Operator: operator,
		Operand:  operand,
	}

	return
}

// This function retrieves the URL of the operator image, operand image and the operator registry image
// built by CI using 'IMAGE_FORMAT' env variable.
func getCIImageURL() (image *Image, err error) {
	format := os.Getenv(ImageFormatVar)
	if format == "" {
		err = fmt.Errorf("ENV var %s not defined", ImageFormatVar)
		return
	}

	image = &Image{
		Format:   format,
		Registry: strings.ReplaceAll(format, ComponentPlaceholder, RegistryName),
		Operator: strings.ReplaceAll(format, ComponentPlaceholder, OperatorName),
		Operand:  strings.ReplaceAll(format, ComponentPlaceholder, OperandName),
	}

	return
}

func read(path string) (object *corev1.ConfigMap, err error) {
	reader, openErr := os.Open(path)
	defer reader.Close()
	if err != nil {
		err = fmt.Errorf("unable to load file %s: %s", path, openErr)
		return
	}

	object, err = decode(reader)
	return
}

func write(path string, object *corev1.ConfigMap) error {
	bytes, err := json.Marshal(object)
	if err != nil {
		return err
	}

	return ioutil.WriteFile(path, bytes, 0644)
}

func decode(reader io.Reader) (object *corev1.ConfigMap, err error) {
	decoder := apimachineryyaml.NewYAMLOrJSONDecoder(reader, 30)

	c := &corev1.ConfigMap{}
	if err = decoder.Decode(c); err != nil {
		return
	}

	object = c
	return
}
