module github.com/jenkins-x-plugins/jx-pipeline

require (
	github.com/GoogleContainerTools/kpt v0.37.1
	github.com/cenkalti/backoff v2.2.1+incompatible
	github.com/charmbracelet/bubbletea v0.13.1
	github.com/cpuguy83/go-md2man v1.0.10
	github.com/fatih/color v1.10.0
	github.com/ghodss/yaml v1.0.1-0.20190212211648-25d852aebe32
	github.com/jenkins-x-plugins/jx-gitops v0.2.60
	github.com/jenkins-x/go-scm v1.6.18
	github.com/jenkins-x/jx-api/v4 v4.0.28
	github.com/jenkins-x/jx-helpers/v3 v3.0.104
	github.com/jenkins-x/jx-kube-client/v3 v3.0.2
	github.com/jenkins-x/jx-logging/v3 v3.0.3
	github.com/jenkins-x/lighthouse-client v0.0.107
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.7.0
	github.com/spf13/cobra v1.1.1
	github.com/spf13/pflag v1.0.5
	github.com/stretchr/testify v1.7.0
	github.com/tektoncd/pipeline v0.20.0
	gocloud.dev v0.21.0
	k8s.io/api v0.20.6
	k8s.io/apimachinery v0.20.6
	k8s.io/client-go v11.0.1-0.20190805182717-6502b5e7b1b5+incompatible
	knative.dev/pkg v0.0.0-20210107022335-51c72e24c179
	sigs.k8s.io/yaml v1.2.0
)

replace (
	// override the go-scm from tekton
	github.com/jenkins-x/go-scm => github.com/jenkins-x/go-scm v1.6.18
	github.com/tektoncd/pipeline => github.com/jenkins-x/pipeline v0.3.2-0.20210118090417-1e821d85abf6
	k8s.io/client-go => k8s.io/client-go v0.20.2
	knative.dev/pkg => github.com/jstrachan/pkg v0.0.0-20210118084935-c7bdd6c14bd0
)

go 1.15
