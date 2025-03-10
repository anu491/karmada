package karmadactl

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/karmada-io/karmada/pkg/karmadactl/cmdinit/utils"
	"github.com/karmada-io/karmada/pkg/karmadactl/options"
)

// LabelSelector karmada bootstrapping label
const (
	LabelSelector    = "karmada.io/bootstrapping"
	karmadaNodeLabel = "karmada.io/etcd"
)

// CommandDeInitOption options for deinit.
type CommandDeInitOption struct {
	options.GlobalCommandOptions
	Namespace     string
	Context       string
	KubeClientSet *kubernetes.Clientset
}

// NewCmdDeInit removes Karmada from Kubernetes
func NewCmdDeInit(cmdOut io.Writer, parentCommand string) *cobra.Command {
	opts := CommandDeInitOption{}
	cmd := &cobra.Command{
		Use:          "deinit",
		Short:        "removes Karmada from Kubernetes",
		Long:         "removes Karmada from Kubernetes",
		Example:      deInitExample(parentCommand),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := opts.Complete(); err != nil {
				return err
			}
			if err := opts.Run(); err != nil {
				return err
			}
			return nil
		},
	}
	opts.GlobalCommandOptions.AddFlags(cmd.Flags())
	flags := cmd.PersistentFlags()
	flags.StringVarP(&opts.Namespace, "namespace", "n", "karmada-system", "namespace where Karmada components are installed.")
	flags.StringVar(&opts.Context, "context", "", "The name of the kubeconfig context to use")
	return cmd
}

func deInitExample(parentCommand string) string {
	example := fmt.Sprintf(`
# Remove Karmada from the Kubernetes cluster.
%s deinit
`, parentCommand)

	return example
}

// Complete the conditions required to be able to run deinit.
func (o *CommandDeInitOption) Complete() error {
	if o.KubeConfig == "" {
		o.KubeConfig = filepath.Join(os.Getenv("HOME"), ".kube/config")
	}

	if !Exists(o.KubeConfig) {
		return ErrEmptyConfig
	}

	restConfig, err := utils.RestConfig(o.Context, o.KubeConfig)
	if err != nil {
		return err
	}

	o.KubeClientSet, err = utils.NewClientSet(restConfig)
	if err != nil {
		return err
	}

	if _, err := o.KubeClientSet.CoreV1().Namespaces().Get(context.TODO(), o.Namespace, metav1.GetOptions{}); err != nil {
		return err
	}

	return nil
}

// delete removes Karmada from Kubernetes
func (o *CommandDeInitOption) delete() error {
	if err := o.deleteWorkload(); err != nil {
		return err
	}

	// Delete Service by label LabelSelector
	serviceClient := o.KubeClientSet.CoreV1().Services(o.Namespace)
	services, err := serviceClient.List(context.TODO(), metav1.ListOptions{LabelSelector: LabelSelector})
	if err != nil {
		return err
	}

	for _, service := range services.Items {
		fmt.Printf("delete Service %q\n", service.Name)
		if o.DryRun {
			continue
		}
		if err := serviceClient.Delete(context.TODO(), service.Name, metav1.DeleteOptions{}); err != nil {
			return err
		}
	}

	// Delete Secret by label LabelSelector
	secretClient := o.KubeClientSet.CoreV1().Secrets(o.Namespace)
	secrets, err := secretClient.List(context.TODO(), metav1.ListOptions{LabelSelector: LabelSelector})
	if err != nil {
		return err
	}
	for _, secret := range secrets.Items {
		fmt.Printf("delete Secrets %q\n", secret.Name)
		if o.DryRun {
			continue
		}
		if err := secretClient.Delete(context.TODO(), secret.Name, metav1.DeleteOptions{}); err != nil {
			return err
		}
	}

	// Delete ClusterRole by label LabelSelector
	clusterRoleClient := o.KubeClientSet.RbacV1().ClusterRoles()
	clusterRoles, err := clusterRoleClient.List(context.TODO(), metav1.ListOptions{LabelSelector: LabelSelector})
	if err != nil {
		return err
	}
	for _, service := range clusterRoles.Items {
		fmt.Printf("delete ClusterRole %q\n", service.Name)
		if o.DryRun {
			continue
		}
		if err := clusterRoleClient.Delete(context.TODO(), service.Name, metav1.DeleteOptions{}); err != nil {
			return err
		}
	}

	return nil
}

func (o *CommandDeInitOption) deleteWorkload() error {
	// Delete deployment by label LabelSelector
	deploymentClient := o.KubeClientSet.AppsV1().Deployments(o.Namespace)
	deployments, err := deploymentClient.List(context.TODO(), metav1.ListOptions{LabelSelector: LabelSelector})
	if err != nil {
		return err
	}
	for _, deployment := range deployments.Items {
		fmt.Printf("delete deployment %q\n", deployment.Name)
		if o.DryRun {
			continue
		}
		if err := deploymentClient.Delete(context.TODO(), deployment.Name, metav1.DeleteOptions{}); err != nil {
			return err
		}
	}

	// Delete StatefulSet by label LabelSelector
	statefulSetClient := o.KubeClientSet.AppsV1().StatefulSets(o.Namespace)
	statefulSets, err := statefulSetClient.List(context.TODO(), metav1.ListOptions{LabelSelector: LabelSelector})
	if err != nil {
		return err
	}

	for _, statefulSet := range statefulSets.Items {
		fmt.Printf("delete StatefulSet: %q\n", statefulSet.Name)
		if o.DryRun {
			continue
		}
		if err := statefulSetClient.Delete(context.TODO(), statefulSet.Name, metav1.DeleteOptions{}); err != nil {
			return err
		}
	}
	return nil
}

// removeNodeLabels removes labels from node which were created by karmadactl init
func (o *CommandDeInitOption) removeNodeLabels() error {
	nodeClient := o.KubeClientSet.CoreV1().Nodes()
	nodes, err := nodeClient.List(context.TODO(), metav1.ListOptions{LabelSelector: karmadaNodeLabel})
	if err != nil {
		return err
	}
	if len(nodes.Items) == 0 {
		fmt.Printf("node not found by label %q\n", karmadaNodeLabel)
		return nil
	}

	for v := range nodes.Items {
		removeLabels(&nodes.Items[v], karmadaNodeLabel)
		fmt.Printf("remove node %q labels %q\n", nodes.Items[v].Name, karmadaNodeLabel)
		if o.DryRun {
			continue
		}
		if _, err := nodeClient.Update(context.TODO(), &nodes.Items[v], metav1.UpdateOptions{}); err != nil {
			return err
		}
	}
	return nil
}

func removeLabels(node *corev1.Node, removesLabel string) {
	for label := range node.Labels {
		if strings.Contains(label, removesLabel) {
			delete(node.Labels, label)
		}
	}
}

// deleteConfirmation delete karmada confirmation
func deleteConfirmation() bool {
	fmt.Println("Please type (y)es or (n)o and then press enter:")
	var response string
	_, err := fmt.Scanln(&response)
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	switch strings.ToLower(response) {
	case "y", "yes":
		return true
	case "n", "no":
		return false
	default:
		return deleteConfirmation()
	}
}

// Run start delete
func (o *CommandDeInitOption) Run() error {
	fmt.Println("removes Karmada from Kubernetes")
	// delete confirmation,exit the delete action when false.
	if !deleteConfirmation() {
		return nil
	}

	if err := o.delete(); err != nil {
		return err
	}

	if err := o.removeNodeLabels(); err != nil {
		return err
	}

	fmt.Println("remove Karmada from Kubernetes successfully." +
		"\ndeinit will not delete etcd data, if the etcd data is persistent, please delete it yourself.")
	return nil
}
