# PMX TalosOS Cluster

Remember to install cilium directly and delete flannel + kubeproxy daemonsets, before flux can be applied.

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