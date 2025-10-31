# PMX TalosOS Cluster

Remember to install cilium directly and delete flannel + kubeproxy daemonsets, before flux can be applied.


## Installation

```sh
cat "$HOME/sops/age/keys.txt" | kubectl --kubeconfig ./kubeconfig.yaml create secret generic sops-age --namespace=flux-system --from-file=age.agekey=/dev/stdin
```

## Debugging

```sh
kubectl --kubeconfig ./kubeconfig.yaml get nodes
kubectl --kubeconfig ./kubeconfig.yaml apply -f infra/cilium/helm-release.yaml

ssh -L 8000:localhost:8000 -N caspertdk@192.168.1.132
talos-tmd-e0p

talosctl --talosconfig ./talosconfig.yaml  --nodes talos-tmd-e0p  get links
talosctl --talosconfig ./talosconfig.yaml  --nodes talos-ska-6at  get links

talosctl --talosconfig ./talosconfig.yaml  --nodes talos-tmd-e0p read /proc/net/route
```

<https://www.itguyjournals.com/installing-cilium-and-multus-on-talos-os-for-advanced-kubernetes-networking/>

## Vm stuff

<https://github.com/kubevirt/kubectl-virt-plugin>
<https://a-cup-of.coffee/blog/omni/>
<https://www.talos.dev/v1.11/advanced/install-kubevirt/>


```sh
kubectl virt --kubeconfig ./kubeconfig.yaml start -n kubevirt-system fedora-vm-test
kubectl virt --kubeconfig ./kubeconfig.yaml stop -n kubevirt-system fedora-vm-test

kubectl virt --kubeconfig ./kubeconfig.yaml start -n kubevirt-system homeserver-vm
kubectl virt --kubeconfig ./kubeconfig.yaml image-upload pvc homeserver-pvc --no-create --image-path=/tmp/images/homeserver-centos-stream9.qcow2

# Upload
kubectl --kubeconfig ./kubeconfig.yaml -n cdi port-forward svc/cdi-uploadproxy 8443:443
kubectl virt --kubeconfig ./kubeconfig.yaml image-upload dv homeserver-installer-dv \
  -n kubevirt-system \
  --no-create \
  --image-path=/tmp/images/homeserver-centos-stream9.qcow2 \
  --uploadproxy-url https://127.0.0.1:8443 \
  --insecure
kubectl virt --kubeconfig ./kubeconfig.yaml -n kubevirt-system get dv homeserver-installer-dv -o yaml | grep phase:

kubectl virt --kubeconfig ./kubeconfig.yaml console -n kubevirt-system fedora-vm-test
```

# install

```sh
kubectl --kubeconfig ./kubeconfig.yaml get nodes -o jsonpath='{range .items[*]}{.metadata.name}{" "}{.spec.podCIDR}{"\n"}{end}'

helm template cilium cilium/cilium \
  --version 1.18.0 \
  --namespace kube-system \
  --set ipam.mode=kubernetes \
  --set kubeProxyReplacement=true \
  --set securityContext.capabilities.ciliumAgent="{CHOWN,KILL,NET_ADMIN,NET_RAW,IPC_LOCK,SYS_ADMIN,SYS_RESOURCE,DAC_OVERRIDE,FOWNER,SETGID,SETUID}" \
  --set securityContext.capabilities.cleanCiliumState="{NET_ADMIN,SYS_ADMIN,SYS_RESOURCE}" \
  --set cgroup.autoMount.enabled=false \
  --set cgroup.hostRoot=/sys/fs/cgroup \
  --set k8sServiceHost=localhost \
  --set k8sServicePort=7445 \
  --set devices="en+" \
  --set nodePort.directRoutingDevice="en+" \
  --set routingMode="native" \
  --set autoDirectNodeRoutes=true \
  --set enableIPv4Masquerade=true \
  --set ipMasqAgent.enabled=false \
  --set ipv4NativeRoutingCIDR="10.244.0.0/16" \
  | tee cilium.yaml
kubectl --kubeconfig ./kubeconfig.yaml apply -f cilium.yaml
```