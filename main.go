package main

import (
	"flag"
	"log"
	"path/filepath"
	"time"

	"github.com/hdkshingala/servicecreator/controller"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {
	var kubeconfig *string

	if home := homedir.HomeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	config, err := rest.InClusterConfig()
	if err != nil {
		log.Printf("Error received while creating config from InCluster, error: %s.\n", err.Error())
		config, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
		if err != nil {
			log.Printf("Error received while creating config from Config, error: %s.\n", err.Error())
			return
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Printf("Error received while creating client set, error: %s.\n", err.Error())
		return
	}

	ch := make(chan struct{})
	informers := informers.NewSharedInformerFactory(clientset, 10*time.Minute)
	c := controller.NewController(clientset, informers.Apps().V1().Deployments())
	informers.Start(ch)
	if err = c.Run(ch); err != nil {
		log.Printf("Error running controller, %s", err.Error())
	}
}
