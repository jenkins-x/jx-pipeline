package fmt

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/jenkins-x/jx-helpers/v3/pkg/cmdrunner"
	"github.com/jenkins-x/jx-helpers/v3/pkg/cobras/helper"
	"github.com/jenkins-x/jx-helpers/v3/pkg/gitclient"
	"github.com/jenkins-x/jx-helpers/v3/pkg/gitclient/cli"
	"github.com/jenkins-x/jx-helpers/v3/pkg/options"
	"github.com/jenkins-x/jx-helpers/v3/pkg/yamls"
	"github.com/pkg/errors"
	"github.com/tektoncd/pipeline/pkg/apis/pipeline/v1alpha1"
	v1beta1 "github.com/tektoncd/pipeline/pkg/apis/pipeline/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/spf13/cobra"

	"github.com/jenkins-x/jx-helpers/v3/pkg/cobras/templates"
)

// StopPipelineOptions contains the command line options
type Options struct {
	options.BaseOptions

	Dir                    string
	Namespace              string
	GitCloneURL            string
	GitClonePullRequestURL string
	CatalogSHA             string
	GitClient              gitclient.Interface
	CommandRunner          cmdrunner.CommandRunner
}

const (
	pipelineCatalogGitURL = "https://github.com/jenkins-x/jx3-pipeline-catalog"
)

var (
	cmdLong = templates.LongDesc(`
		Formats the local pipeline files

		* removes any unnecessary parameters
		* converts any shell commands to use 'script:' notation
`)

	cmdExample = templates.Examples(`
		# Formats the local pipeline files
		jx pipeline fmt
	`)

	removeStepNames = map[string]bool{
		"git-clone":          true,
		"git-merge":          true,
		"git-setup":          true,
		"setup-builder-home": true,
	}

	shellBinaries = map[string]bool{
		"/bin/ash":    true,
		"/bin/bash":   true,
		"/bin/sh":     true,
		"/busybox/sh": true,
	}
)

// NewCmdPipelineFormat creates the command
func NewCmdPipelineFormat() (*cobra.Command, *Options) {
	o := &Options{}

	cmd := &cobra.Command{
		Use:     "fmt",
		Short:   "Formats the local pipeline files",
		Long:    cmdLong,
		Example: cmdExample,
		Run: func(cmd *cobra.Command, args []string) {
			err := o.Run()
			helper.CheckErr(err)
		},
	}
	cmd.Flags().StringVarP(&o.Dir, "dir", "d", ".lighthouse", "The directory to look for the tekton YAML files")
	cmd.Flags().StringVarP(&o.CatalogSHA, "sha", "", "", "The git commit SHA of the pipeline catalog repository "+pipelineCatalogGitURL+". If not specified we clone git and find it")
	return cmd, o
}

// Validate validate options
func (o *Options) Validate() error {
	if o.GitClient == nil {
		if o.CommandRunner == nil {
			o.CommandRunner = cmdrunner.QuietCommandRunner
		}
		o.GitClient = cli.NewCLIClient("", o.CommandRunner)
	}
	if o.CatalogSHA == "" {
		dir, err := gitclient.CloneToDir(o.GitClient, pipelineCatalogGitURL, "")
		if err != nil {
			return errors.Wrapf(err, "failed to clone %s", pipelineCatalogGitURL)
		}
		o.CatalogSHA, err = gitclient.GetLatestCommitSha(o.GitClient, dir)
		if err != nil {
			return errors.Wrapf(err, "failed to get latest commit sha from clone of %s in dir %s", pipelineCatalogGitURL, dir)
		}
	}
	if o.GitCloneURL == "" {
		o.GitCloneURL = "https://raw.githubusercontent.com/jenkins-x/jx3-pipeline-catalog/" + o.CatalogSHA + "/tasks/git-clone/git-clone.yaml"
	}
	if o.GitClonePullRequestURL == "" {
		o.GitClonePullRequestURL = "https://raw.githubusercontent.com/jenkins-x/jx3-pipeline-catalog/" + o.CatalogSHA + "/tasks/git-clone/git-clone-pr.yaml"
	}
	return nil
}

// Run implements this command
func (o *Options) Run() error {
	err := o.Validate()
	if err != nil {
		return errors.Wrapf(err, "failed to validate options")
	}
	dir := o.Dir
	err = filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if info == nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".yaml") {
			return nil
		}
		return o.processFile(path)
	})
	if err != nil {
		return errors.Wrapf(err, "failed to process YAML files in dir %s", dir)
	}
	return nil
}

func (o *Options) processFile(path string) error {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return errors.Wrapf(err, "failed to load file %s", path)
	}

	kindPrefix := "kind:"
	kind := ""
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if !strings.HasPrefix(line, kindPrefix) {
			continue
		}
		k := strings.TrimSpace(line[len(kindPrefix):])
		if k != "" {
			kind = k
			break
		}
	}
	message := "processing file %s" + path
	switch kind {
	case "Pipeline":
		pipeline := &v1beta1.Pipeline{}
		err := yaml.Unmarshal(data, pipeline)
		if err != nil {
			return errors.Wrapf(err, "failed to unmarshal Pipeline YAML %s", message)
		}
		return o.processPipeline(pipeline, path)

	case "PipelineRun":
		prs := &v1beta1.PipelineRun{}
		err := yaml.Unmarshal(data, prs)
		if err != nil {
			return errors.Wrapf(err, "failed to unmarshal PipelineRun YAML %s", message)
		}
		return o.processPipelineRun(prs, path)

	case "Task":
		task := &v1beta1.Task{}
		err := yaml.Unmarshal(data, task)
		if err != nil {
			return errors.Wrapf(err, "failed to unmarshal Task YAML %s", message)
		}
		return o.processTask(task, path)

	default:
		return nil
	}
}

func (o *Options) processPipelineRun(prs *v1beta1.PipelineRun, path string) error {
	ps := &prs.Spec
	_, name := filepath.Split(path)
	prs.Name = strings.TrimSuffix(name, ".yaml")
	prs.Labels = nil
	if prs.Annotations == nil {
		prs.Annotations = map[string]string{}
	}
	gitCloneURL := o.GitCloneURL
	if prs.Name != "release" {
		gitCloneURL = o.GitClonePullRequestURL
	}
	prs.Annotations["lighthouse.jenkins-x.io/prependStepsURL"] = gitCloneURL

	if ps.PipelineSpec != nil {
		err := o.processPipelineSpec(ps.PipelineSpec, path)
		if err != nil {
			return errors.Wrapf(err, "failed to ")
		}
	}
	err := yamls.SaveFile(prs, path)
	if err != nil {
		return errors.Wrapf(err, "failed to save file %s", path)
	}
	return nil
}

func (o *Options) processPipelineSpec(spec *v1beta1.PipelineSpec, path string) error {
	var ss *corev1.Container
	spec.Params = RemoveDefaultParamSpecs(spec.Params)
	for i := range spec.Tasks {
		task := &spec.Tasks[i]
		task.Params = RemoveDefaultParams(task.Params)
		ts := task.TaskSpec
		if ts != nil {
			ts.Params = RemoveDefaultParamSpecs(ts.Params)
			var steps []v1beta1.Step
			for j := range ts.Steps {
				s := ts.Steps[j]
				if !removeStepNames[s.Name] {
					s = o.convertToScriptStep(s)
					steps = append(steps, s)
				}
			}
			ts.Steps = steps
		}
		if ts != nil {
			ss = ts.StepTemplate
		}
		if ss != nil {
			ss.Env = RemoveDefaultEnvVars(ss.Env)
		}
	}
	return nil
}

func (o *Options) processPipeline(pipeline *v1beta1.Pipeline, path string) error {
	err := o.processPipelineSpec(&pipeline.Spec, path)
	if err != nil {
		return errors.Wrapf(err, "failed to ")
	}
	err = yamls.SaveFile(pipeline, path)
	if err != nil {
		return errors.Wrapf(err, "failed to save file %s", path)
	}
	return nil
}

func (o *Options) processTask(task *v1beta1.Task, path string) error {
	return nil
}

func (o *Options) convertToScriptStep(s v1beta1.Step) v1beta1.Step {
	if len(s.Command) == 0 || len(s.Args) == 0 {
		return s
	}
	bin := s.Command[0]
	arg := ""
	if bin == "jx" {
		arg = "jx " + strings.Join(s.Args, " ")
		bin = "/usr/bin/env bash"
	} else {
		if !shellBinaries[bin] {
			return s
		}
		if len(s.Command) == 2 && s.Command[1] == "-c" && len(s.Args) == 1 {
			arg = s.Args[0]
		} else if len(s.Command) == 1 && len(s.Args) == 2 && s.Args[0] == "-c" {
			arg = s.Args[1]
		}
	}
	if arg == "" {
		return s
	}
	if strings.HasPrefix(arg, "jx ") {
		bin = "/usr/bin/env bash"
	}
	s.Command = nil
	s.Args = nil
	s.Script = "#!" + bin + "\n" + strings.ReplaceAll(arg, " && ", "\n") + "\n"
	return s
}

// RemoveDefaultParamSpecs removes default parameters
func RemoveDefaultParamSpecs(from []v1alpha1.ParamSpec) []v1alpha1.ParamSpec {
	var params []v1beta1.ParamSpec
	for _, p := range from {
		if !defaultParameterNames[p.Name] {
			params = append(params, p)
		}
	}
	return params
}

// RemoveDefaultParams removes default params
func RemoveDefaultParams(from []v1alpha1.Param) []v1alpha1.Param {
	var params []v1beta1.Param
	for _, p := range from {
		if !defaultParameterNames[p.Name] {
			params = append(params, p)
		}
	}
	return params
}

// RemoveDefaultEnvVars removes default params
func RemoveDefaultEnvVars(from []corev1.EnvVar) []corev1.EnvVar {
	var answer []corev1.EnvVar
	for _, p := range from {
		if !defaultParameterNames[p.Name] {
			answer = append(answer, p)
		}
	}
	return answer
}
