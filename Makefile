REPO := github.com/lichuan0620/k8s-controller-the-hard-way

.PHONY: kubernetes-gen clientset-gen crd-gen

kubernetes-gen: clientset-gen crd-gen

clientset-gen:
	@/bin/bash ./scripts/generate-clientset.sh all \
	$(REPO)/pkg/kubernetes/client $(REPO)/pkg/kubernetes/apis demo:v1 \
	--go-header-file pkg/kubernetes/apis/demo/boilerplate/boilerplate.go.txt

crd-gen:
	@go install sigs.k8s.io/controller-tools/cmd/controller-gen@v0.8.0
	@controller-gen crd paths=./pkg/kubernetes/apis/... output:crd:artifacts:config=crds
