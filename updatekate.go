package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/blang/semver"
	"github.com/coreos/pkg/flagutil"
	"io/ioutil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"log"
	"net/http"
	"strings"
	"time"
)

type K8Client struct {
	clientset  *kubernetes.Clientset
	namespace  string
	deployment string
	dockerRepo string
	webhook    string
}

func main() {

	flagSet := flag.NewFlagSet("updatekate", flag.ExitOnError)

	namespace := flag.String("namespace", "ec", "The namespace of the deployment to update")
	deployment := flag.String("deployment", "ec-deployment", "The deployment to update")
	dockerRepo := flag.String("repo", "boundlessgeo/geoserver-ec", "The docker repo to watch")
	webhook := flag.String("webhook", "", "A webhook to invoke upon successful update")
	//baseImage := flag.String("repository","","The name of the repository to allow -- if empty then any repo is allowed")
	//port := flag.String("port",":8888","The port to listen on")
	var port = ":8888"

	//only ENV
	if err := flagutil.SetFlagsFromEnv(flagSet, "UK"); err != nil {
		log.Fatal()
	}

	//setup the k8s client config
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatal("This only works inside of K8S!!")
	}

	// creates the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatal("Error getting k8s client ")
	}
	k8 := K8Client{clientset: clientset, namespace: *namespace, deployment: *deployment, dockerRepo: *dockerRepo, webhook: *webhook}

	http.HandleFunc("/webhook", k8.updateWebhook)
	http.HandleFunc("/info", k8.getInfo)
	http.ListenAndServe(port, nil)
}

func (k8 *K8Client) updateWebhook(w http.ResponseWriter, r *http.Request) {

	if r.Method != "POST" {
		w.WriteHeader(405) //method not allowed
		return
	}
	qn := new(QuayNotification)

	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading body of POST")
	}
	json.Unmarshal(body, &qn)

	if k8.dockerRepo != qn.Repository {
		log.Printf("NOT going to update deployment! Expected repo %v and got repo %v", k8.dockerRepo, qn.Repository)
		w.WriteHeader(409) //conflict seems appropriate
		return
	}

	result, err := k8.clientset.AppsV1beta1().Deployments(k8.namespace).Get(k8.deployment, metav1.GetOptions{})
	if err != nil {
		log.Printf("Error retrieving deployment info: %v", err.Error())
		return
	}

	currentImage := result.Spec.Template.Spec.Containers[0].Image
	currentVersionTag := currentImage[strings.LastIndex(currentImage, ":")+1:]
	currentVersion, _ := semver.Make(currentVersionTag)

	log.Printf("Current major: %v", currentVersion.Major)
	log.Printf("Current minor: %v", currentVersion.Minor)
	log.Printf("Current path: %v", currentVersion.Patch)
	log.Printf("Current build: %v", currentVersion.Build)
	log.Printf("Current version string: %v", currentVersion.String())

	log.Printf("Current version of image is %v", currentVersionTag)
	var newTag string
	for _, newVer := range qn.UpdatedTags {
		//we don't want latest tags because K8s won't re-pull them consistently
		if newVer == "latest" {
			continue
		}

		tagVer, _ := semver.Make(newVer)
		if currentVersion.LT(tagVer) {
			newTag = newVer
			log.Printf("Found newer version of image - %v", newTag)
			log.Printf("New major: %v", tagVer.Major)
			log.Printf("New minor: %v", tagVer.Minor)
			log.Printf("New path: %v", tagVer.Patch)
			log.Printf("New build: %v", tagVer.Build)
			log.Printf("New pre: %v", tagVer.Pre)
			log.Printf("New version string: %v", tagVer.String())

			go k8.update(newVer)

			break
		}
	}

	//punt for now
	w.WriteHeader(200)

}
func (k8 *K8Client) update(newVersion string) {

	log.Println("Starting update....")
	result, err := k8.clientset.AppsV1beta1().Deployments(k8.namespace).Get(k8.deployment, metav1.GetOptions{})
	//error...abort
	if err != nil {
		log.Printf("Error fetching deployment: %v", err.Error())
		return
	}
	log.Println("Updating container image to: $v", k8.dockerRepo+":"+newVersion)
	result.Spec.Template.Spec.Containers[0].Image = k8.dockerRepo + ":" + newVersion
	log.Println("Applying update")
	_, err = k8.clientset.AppsV1beta1().Deployments(k8.namespace).Update(result)

	//error..abort
	if err != nil {
		log.Printf("Error applying update: %v", err.Error())
		return
	}

	//wait until status is good...
	var retryCount int
	for {
		retryCount++
		//if we hit this and have tried 10 times with no success then give up
		if retryCount == 10 {
			log.Println("Backoff retry expired before sucessfully updating deployment - webhook will not fire")
			break
		}

		updatedDep, err := k8.clientset.AppsV1beta1().Deployments(k8.namespace).Get(k8.deployment, metav1.GetOptions{})
		if err != nil {
			//wait an retry...
			log.Print("Error fetching status...sleeping")
			time.Sleep(time.Duration(int64(5000 * retryCount)))
			continue
		}
		//wait until at least 1 replica is ready
		if updatedDep.Status.ReadyReplicas == 0 {
			log.Print("No containers ready...sleeping")
			time.Sleep(time.Duration(int64(5000 * retryCount)))
			continue

		} else {
			log.Println("Deployment updated!")
			log.Print(json.Marshal(updatedDep.Status))
			up := UpdatekateNotification{Timestamp: time.Now().String(), Deployment: k8.deployment, Namespace: k8.namespace, Image: k8.dockerRepo + ":" + newVersion}
			go k8.doWebhook(&up)
			break

		}

	}

}
func (k8 *K8Client) doWebhook(notification *UpdatekateNotification) {
	if k8.webhook != "" {
		payload, _ := json.Marshal(notification)
		http.DefaultClient.Post(k8.webhook, "application/json", bytes.NewReader(payload))
	}

}

func (k8 *K8Client) getInfo(w http.ResponseWriter, r *http.Request) {

	result, err := k8.clientset.AppsV1beta1().Deployments(k8.namespace).Get(k8.deployment, metav1.GetOptions{})
	if err != nil {
		out := []byte(fmt.Sprintf("Not able to find %v deployment in %v namespace", k8.deployment, k8.namespace))
		w.Write(out)
		return
	} else {
		out, _ := json.Marshal(result)
		w.Header().Add("Content-Type", "application/json;charset=UTF-8")
		w.WriteHeader(200)
		w.Write(out)
	}

}

type UpdatekateNotification struct {
	Timestamp  string `json:"timestamp"`
	Deployment string `json:"repository"`
	Namespace  string `json:"namespace"`
	Image      string `json:"docker_url"`
}

type QuayNotification struct {
	Name        string   `json:"name"`
	Repository  string   `json:"repository"`
	Namespace   string   `json:"namespace"`
	DockerURL   string   `json:"docker_url"`
	Homepage    string   `json:"homepage"`
	UpdatedTags []string `json:"updated_tags"`
}
