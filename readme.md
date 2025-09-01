# PMX TalosOS Cluster

Remember to install cilium directly and delete flannel + kubeproxy daemonsets, before flux can be applied.

## Debugging

```sh
kubectl --kubeconfig ./talos-default-kubeconfig.yaml get nodes
ssh -L 8000:localhost:8000 -N caspertdk@192.168.1.132
talos-tmd-e0p
```