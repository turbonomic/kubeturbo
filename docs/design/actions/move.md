# Pod move
move a pod which is controlled by a ReplicationController, or ReplicaSet, or Deployment(via ReplicaSet).

# Three steps
 1. Create a clone Pod (**podCloned**) of the original Pod (**podOrig**)
 
   The cloned pod **podCloned** has everything except selected labels (those with keys `pod-template-hash`, `deployment` and `deploymentconfig`) and podName from the original Pod. **podCloned** is given a new name suffixing an unique key to the podName of **podOrig**. The selected labels are not added to the clone pod to ensure it does not "yet" get adopted by the parent controller.
   
 2. Delete the original Pod
 
   Wait for the new pod (**podCloned**) to be ready and delete the original pod (**podOrig**). Once the **podOrig** get deleted, the controller will immediately create another new Pod **podB** to ensure the replicas match the desired number of replicas for the parent controller.
   
 3. Update the new Pod **podCloned** by adding all the labels back 
 
   Once **podCloned** get the labels, the matched controller will try to [adopt it](https://github.com/kubernetes/kubernetes/blob/fa557ee7921fc8305d4978e66eb653c92ed1a7ce/pkg/controller/replicaset/replica_set.go#L333). After [the adoption](https://github.com/kubernetes/kubernetes/blob/4beb0c2f8634054950cb7ca0b4c24a12aadc612e/pkg/controller/replicaset/replica_set.go#L616), the number of living pods for the controller will be one more than the specified replica number, so controller will [select one pod to delete](https://github.com/kubernetes/kubernetes/blob/4beb0c2f8634054950cb7ca0b4c24a12aadc612e/pkg/controller/replicaset/replica_set.go#L623). The default selection policy is to choose the latest pod first to delete to reduce the number of replicas to match the desired replica number.
   As **podCloned** is older than **podB**, **podB** will get deleted. The **podOrig** is now thus transformed to **podCloned**.
 
 
