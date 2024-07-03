Change namespace used in metric for control-plane shoot pods

The kube-system namespace was previously used, but this was misleading
because those pods are not deployed in the kube-system of the shoot, but
in a specific namespace in the seed. We change this namespace to
control-plane now so that it better represents the namespace in the
seed. Note this is still however a fake namespace that does not really
exist.

# maybe: shoot-control-plane, mention the motivation: blackbox-exporter exists both in the control plane and in the shoot's kube-system namespace and without this change, they can not be distinguished
