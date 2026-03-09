package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mattbaird/jsonpatch"
	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

func init() {
	_ = corev1.AddToScheme(runtimeScheme)
	_ = admissionv1.AddToScheme(runtimeScheme)
}

func main() {
	r := gin.Default()
	r.POST("/mutate", mutateHandler)

	srv := &http.Server{
		Addr:    ":8443",
		Handler: r,
	}

	go func() {
		log.Println("Gin mutating webhook listening on :8443")
		if err := srv.ListenAndServeTLS("/etc/webhook/certs/tls.crt", "/etc/webhook/certs/tls.key"); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Webhook failed to start: %s\n", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	<-quit

	log.Println("Received termination signal, shutting down webhook service gracefully...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("Webhook service forced shutdown error: ", err)
	}

	log.Println("Webhook service successfully stopped!")
}

func mutateHandler(c *gin.Context) {
	var review admissionv1.AdmissionReview
	if err := c.ShouldBindJSON(&review); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid admission review"})
		return
	}

	if review.Request == nil || review.Request.Object.Raw == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no object in request"})
		return
	}

	pod := &corev1.Pod{}
	if err := json.Unmarshal(review.Request.Object.Raw, pod); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unmarshal pod failed"})
		return
	}

	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}

	for i := range pod.Spec.Containers {
		container := &pod.Spec.Containers[i]
		if limit, ok := container.Resources.Limits[corev1.ResourceEphemeralStorage]; ok && limit.String() != "" {
			key := "storage.terminus.io/size." + container.Name
			pod.Annotations[key] = limit.String()
		}
	}

	modifiedPodBytes, err := json.Marshal(pod)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "marshal patched pod failed"})
		return
	}

	patch, err := jsonpatch.CreatePatch(review.Request.Object.Raw, modifiedPodBytes)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create patch failed"})
		return
	}

	jsonPatchBytes, err := json.Marshal(patch)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "marshal json patch failed"})
		return
	}

	resp := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			Kind:       "AdmissionReview",
			APIVersion: "admission.k8s.io/v1",
		},
		Response: &admissionv1.AdmissionResponse{
			UID:     review.Request.UID,
			Allowed: true,
			Patch:   jsonPatchBytes,
			PatchType: func() *admissionv1.PatchType {
				pt := admissionv1.PatchTypeJSONPatch
				return &pt
			}(),
		},
	}

	c.JSON(http.StatusOK, resp)
}
