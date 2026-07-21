--------------------------- MODULE ReplicaController ---------------------------
(* Abstract specification of a ReplicaSet-style controller.                   *)
(*                                                                            *)
(* The environment changes the desired replica count and crashes pods; the   *)
(* controller converges the running pod set toward the desired count, one    *)
(* pod per reconcile step.                                                   *)
EXTENDS Naturals, FiniteSets

CONSTANTS Pods,        \* the pool of pod names, e.g. {"p1", "p2", "p3"}
          MaxReplicas  \* upper bound on the desired count

VARIABLES desired, \* the desired replica count (from the object's spec)
          pods     \* the set of running pods (observed cluster state)

vars == <<desired, pods>>

TypeOK == /\ desired \in 0..MaxReplicas
          /\ pods \subseteq Pods

Init == /\ desired = 1
        /\ pods = {}

(* Environment: a user updates the desired count.                             *)
ChangeDesired == /\ \E d \in 0..MaxReplicas : /\ d /= desired
                                              /\ desired' = d
                 /\ UNCHANGED pods

(* Environment: a running pod dies.                                           *)
PodCrash == /\ \E p \in pods : pods' = pods \ {p}
            /\ UNCHANGED desired

(* Controller: create one pod when under the desired count.                   *)
CreatePod == /\ Cardinality(pods) < desired
             /\ \E p \in Pods \ pods : pods' = pods \cup {p}
             /\ UNCHANGED desired

(* Controller: delete one pod when over the desired count.                    *)
DeletePod == /\ Cardinality(pods) > desired
             /\ \E p \in pods : pods' = pods \ {p}
             /\ UNCHANGED desired

Next == ChangeDesired \/ PodCrash \/ CreatePod \/ DeletePod

Spec == Init /\ [][Next]_vars /\ WF_vars(CreatePod) /\ WF_vars(DeletePod)

(* Safety: even under churn and crashes, the controller never provisions     *)
(* more pods than the configured maximum.                                    *)
NeverOverProvision == Cardinality(pods) <= MaxReplicas

(* Liveness, checked in a quiescent world: starting from ANY type-correct    *)
(* state, if the environment goes quiet (no spec changes, no crashes), fair  *)
(* reconciliation converges the pod count to the desired count and keeps it  *)
(* there.                                                                    *)
QuiescentNext == CreatePod \/ DeletePod

QuiescentInit == /\ desired \in 0..MaxReplicas
                 /\ pods \in SUBSET Pods

QuiescentSpec == /\ QuiescentInit
                 /\ [][QuiescentNext]_vars
                 /\ WF_vars(CreatePod)
                 /\ WF_vars(DeletePod)

Convergence == <>[](Cardinality(pods) = desired)
================================================================================
