package effective

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jenkins-x/jx-helpers/v3/pkg/cmdrunner"
	"github.com/jenkins-x/jx-helpers/v3/pkg/input"
	"github.com/jenkins-x/jx-helpers/v3/pkg/input/inputfactory"
	"github.com/jenkins-x/jx-helpers/v3/pkg/options"
	"github.com/jenkins-x/jx-logging/v3/pkg/log"
	"github.com/jenkins-x/jx-pipeline/pkg/lighthouses"
	tektonv1beta1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	"sigs.k8s.io/yaml"

	"github.com/jenkins-x/jx-helpers/v3/pkg/cobras/helper"
	"github.com/jenkins-x/jx-helpers/v3/pkg/cobras/templates"
	"github.com/jenkins-x/jx-helpers/v3/pkg/files"
	"github.com/jenkins-x/jx-helpers/v3/pkg/termcolor"
	"github.com/jenkins-x/jx-helpers/v3/pkg/yamls"
	"github.com/jenkins-x/lighthouse-client/pkg/config/job"
	"github.com/jenkins-x/lighthouse-client/pkg/triggerconfig"
	"github.com/jenkins-x/lighthouse-client/pkg/triggerconfig/inrepo"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// Options contains the command line options
type Options struct {
	options.BaseOptions
	lighthouses.ResolverOptions

	Namespace     string
	OutFile       string
	TriggerName   string
	PipelineName  string
	Editor        string
	Line          string
	Recursive     bool
	Resolver      *inrepo.UsesResolver
	Triggers      []*Trigger
	Input         input.Interface
	CommandRunner cmdrunner.CommandRunner
}

var (
	info = termcolor.ColorInfo

	cmdLong = templates.LongDesc(`
		Displays the effective tekton pipeline
`)

	cmdExample = templates.Examples(`
		# View the effective pipeline 
		jx pipeline effective

		# View the effective pipeline in VS Code 
		jx pipeline effective -e code

		# View the effective pipeline in IDEA 
		jx pipeline effective -e idea

		# Enable open in VS Code
 		export JX_EDITOR="code"
		jx pipeline effective
	`)
)

// Trigger the found trigger configs
type Trigger struct {
	Path      string
	Config    *triggerconfig.Config
	Names     []string
	Pipelines map[string]*tektonv1beta1.PipelineRun
}

// NewCmdPipelineEffective creates the command
func NewCmdPipelineEffective() (*cobra.Command, *Options) {
	o := &Options{}

	cmd := &cobra.Command{
		Use:     "effective",
		Short:   "Displays the effective tekton pipeline",
		Long:    cmdLong,
		Example: cmdExample,
		Aliases: []string{"dump"},
		Run: func(cmd *cobra.Command, args []string) {
			err := o.Run()
			helper.CheckErr(err)
		},
	}

	o.ResolverOptions.AddFlags(cmd)

	cmd.Flags().StringVarP(&o.TriggerName, "trigger", "t", "", "The path to the trigger file. If not specified you will be prompted to choose one")
	cmd.Flags().StringVarP(&o.PipelineName, "pipeline", "p", "", "The pipeline kind and name. e.g. 'presubmit/pr' or 'postsubmit/release'. If not specified you will be prompted to choose one")
	cmd.Flags().StringVarP(&o.OutFile, "out", "o", "", "The output file to write the effective pipeline to. If not specified output to the terminal")
	cmd.Flags().StringVarP(&o.Editor, "editor", "e", "", "The editor to open the effective pipeline inside. e.g. use 'idea' or 'code'")
	cmd.Flags().StringVarP(&o.Line, "line", "", "", "The line number to open the editor at")
	cmd.Flags().BoolVarP(&o.Recursive, "recursive", "r", false, "Recurisvely find all '.lighthouse' folders such as if linting a Pipeline Catalog")

	o.BaseOptions.AddBaseFlags(cmd)
	return cmd, o

}

// Validate verifies settings
func (o *Options) Validate() error {
	err := o.BaseOptions.Validate()
	if err != nil {
		return errors.Wrapf(err, "failed to validate base options")
	}
	if o.Input == nil {
		o.Input = inputfactory.NewInput(&o.BaseOptions)
	}
	if o.Resolver == nil {
		o.Resolver, err = o.ResolverOptions.CreateResolver()
		if err != nil {
			return errors.Wrapf(err, "failed to create a UsesResolver")
		}
	}
	if o.CommandRunner == nil {
		o.CommandRunner = cmdrunner.QuietCommandRunner
	}
	if o.Editor == "" {
		o.Editor = os.Getenv("JX_EDITOR")
	}
	return nil
}

// Run implements this command
func (o *Options) Run() error {
	err := o.Validate()
	if err != nil {
		return errors.Wrapf(err, "failed to validate options")
	}

	rootDir := o.Dir

	if o.Recursive {
		err := filepath.Walk(rootDir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info == nil || !info.IsDir() || info.Name() != ".lighthouse" {
				return nil
			}
			return o.ProcessDir(path)
		})
		if err != nil {
			return err
		}
	} else {
		dir := filepath.Join(rootDir, ".lighthouse")
		err := o.ProcessDir(dir)
		if err != nil {
			return err
		}
	}
	return o.processTriggers()
}

func (o *Options) ProcessDir(dir string) error {
	fs, err := ioutil.ReadDir(dir)
	if err != nil {
		return errors.Wrapf(err, "failed to read dir %s", dir)
	}
	for _, f := range fs {
		name := f.Name()
		if !f.IsDir() || strings.HasPrefix(name, ".") {
			continue
		}

		triggerDir := filepath.Join(dir, name)
		triggersFile := filepath.Join(triggerDir, "triggers.yaml")
		exists, err := files.FileExists(triggersFile)
		if err != nil {
			return errors.Wrapf(err, "failed to check if file exists %s", triggersFile)
		}
		if !exists {
			continue
		}
		triggers := &triggerconfig.Config{}
		err = yamls.LoadFile(triggersFile, triggers)
		if err != nil {
			return errors.Wrapf(err, "failed to load %s", triggersFile)
		}
		trigger := &Trigger{
			Path:      triggersFile,
			Config:    triggers,
			Pipelines: map[string]*tektonv1beta1.PipelineRun{},
		}
		o.Triggers = append(o.Triggers, trigger)

		err = o.loadTriggerPipelines(trigger, triggerDir)
		if err != nil {
			return errors.Wrapf(err, "failed to load pipelines for trigger: %s", triggersFile)
		}
	}
	return nil
}

func (o *Options) loadTriggerPipelines(trigger *Trigger, dir string) error {
	repoConfig := trigger.Config
	for i := range repoConfig.Spec.Presubmits {
		r := &repoConfig.Spec.Presubmits[i]
		if r.SourcePath != "" {
			path := filepath.Join(dir, r.SourcePath)
			pr, err := lighthouses.LoadEffectivePipelineRun(o.Resolver, path)
			if err != nil {
				return errors.Wrapf(err, "failed to load %s", path)
			}
			name := "presubmit/" + r.Name
			trigger.Names = append(trigger.Names, name)
			trigger.Pipelines[name] = pr
		}
		if r.Agent == "" && r.PipelineRunSpec != nil {
			r.Agent = job.TektonPipelineAgent
		}
	}
	for i := range repoConfig.Spec.Postsubmits {
		r := &repoConfig.Spec.Postsubmits[i]
		if r.SourcePath != "" {
			path := filepath.Join(dir, r.SourcePath)
			pr, err := lighthouses.LoadEffectivePipelineRun(o.Resolver, path)
			if err != nil {
				return errors.Wrapf(err, "failed to load %s", path)
			}
			name := "postsubmit/" + r.Name
			trigger.Names = append(trigger.Names, name)
			trigger.Pipelines[name] = pr
		}
		if r.Agent == "" && r.PipelineRunSpec != nil {
			r.Agent = job.TektonPipelineAgent
		}
	}
	return nil
}

func (o *Options) processTriggers() error {
	var names []string
	m := map[string]*Trigger{}
	for _, trigger := range o.Triggers {
		name := trigger.Path
		names = append(names, name)
		m[name] = trigger
	}

	var err error
	name := o.TriggerName
	if name == "" {
		name, err = o.Input.PickNameWithDefault(names, "pick the trigger config: ", "", "select the set of triggers to process")
		if err != nil {
			return errors.Wrapf(err, "failed to pick trigger file")
		}
		if name == "" {
			return errors.Errorf("no trigger file selected")
		}
	}
	trigger := m[name]
	if trigger == nil {
		return options.InvalidOptionf("trigger", o.TriggerName, "available names %s", strings.Join(names, ", "))
	}

	pipelineName := o.PipelineName
	if pipelineName == "" {
		pipelineName, err = o.Input.PickNameWithDefault(trigger.Names, "pick the pipeline: ", "", "select the pipeline to view")
		if err != nil {
			return errors.Wrapf(err, "failed to pick trigger file")
		}
		if pipelineName == "" {
			return errors.Errorf("no trigger file selected")
		}
	}
	pipeline := trigger.Pipelines[pipelineName]
	if pipeline == nil {
		return options.InvalidOptionf("pipeline", o.PipelineName, "available names %s", strings.Join(trigger.Names, ", "))
	}

	return o.displayPipeline(trigger, pipelineName, pipeline)
}

func (o *Options) displayPipeline(trigger *Trigger, name string, pipeline *tektonv1beta1.PipelineRun) error {
	// lets create an output file if using editor
	if o.Editor != "" && o.OutFile == "" {
		fileName := ""
		absRootDir, err := filepath.Abs(o.Dir)
		if err == nil {
			_, fileName = filepath.Split(absRootDir)
		}
		if fileName == "" || len(fileName) == 1 {
			fileName = "jx-pipeline"
		}
		tmpFileName := fileName + "-" + strings.ReplaceAll(name, string(os.PathSeparator), "-") + "-*.yaml"
		tmpFile, err := ioutil.TempFile("", tmpFileName)
		if err != nil {
			return errors.Wrapf(err, "failed to create temp file")
		}
		o.OutFile = tmpFile.Name()
	}

	if o.OutFile != "" {
		err := yamls.SaveFile(pipeline, o.OutFile)
		if err != nil {
			return errors.Wrapf(err, "failed to save file %s", o.OutFile)
		}
		log.Logger().Infof("saved file %s", info(o.OutFile))

		if o.Editor != "" {
			return o.openInEditor(o.OutFile, o.Editor)
		}
		return nil
	}

	data, err := yaml.Marshal(pipeline)
	if err != nil {
		return errors.Wrapf(err, "failed to marshal pipeline for %s", name)
	}

	log.Logger().Infof("trigger %s pipeline %s", info(trigger.Path), info(name))
	fmt.Print(string(data))
	return nil
}

func (o *Options) openInEditor(path string, editor string) error {
	args := []string{path}
	line := o.Line
	if line == "" {
		var err error
		line, err = findFirstStepLine(path)
		if err != nil {
			return errors.Wrapf(err, "failed to ")
		}
		if line == "" {
			// lets guess an approximate place after all the parameters
			line = "161"
		}
	}
	if line != "" {
		switch editor {
		case "idea":
			args = []string{"--line", line, path}
		case "code":
			args = []string{"-g", path + ":" + line}
		}
	}

	c := &cmdrunner.Command{
		Name: editor,
		Args: args,
		Out:  os.Stdout,
		Err:  os.Stderr,
		In:   os.Stdin,
	}
	_, err := o.CommandRunner(c)
	if err != nil {
		return errors.Wrapf(err, "failed to open editor via command: %s", c.CLI())
	}
	return nil
}

func findFirstStepLine(path string) (string, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return "", errors.Wrapf(err, "failed to load pipeline file %s", path)
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "steps:" {
			return strconv.Itoa(i + 2), nil
		}
	}
	log.Logger().Infof("could not find line with 'steps:'")
	return "", nil
}
