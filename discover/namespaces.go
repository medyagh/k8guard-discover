package discover

import (
	"encoding/json"
	"strings"

	"github.com/k8guard/k8guard-discover/metrics"
	"github.com/k8guard/k8guard-discover/rules"
	lib "github.com/k8guard/k8guardlibs"
	"github.com/k8guard/k8guardlibs/messaging/kafka"
	"github.com/k8guard/k8guardlibs/violations"
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/pkg/api/v1"
)

func isIgnoredNamespace(namespace string) bool {
	for _, n := range lib.Cfg.IgnoredNamespaces {
		if n == namespace {
			return true
		}
	}
	return false
}

func GetAllNamespacesFromApi() []v1.Namespace {
	namespaces := Clientset.Namespaces()

	namespaceList, err := namespaces.List(metav1.ListOptions{})

	if err != nil {
		lib.Log.Error("error: ", err)
		panic(err.Error())
	}

	metrics.Update(metrics.ALL_NAMESPACE_COUNT, len(namespaceList.Items))

	return namespaceList.Items
}

func GetBadNamespaces(theNamespaces []v1.Namespace, sendToKafka bool) []lib.Namespace {
	timer := prometheus.NewTimer(prometheus.ObserverFunc(metrics.FNGetBadNamespaces.Set))
	defer timer.ObserveDuration()

	allBadNamespaces := []lib.Namespace{}

	allBadNamespaces = append(allBadNamespaces, verifyRequiredNamespaces(theNamespaces)...)

	for _, kn := range theNamespaces {
		if isIgnoredNamespace(kn.Namespace) == true {
			continue
		}
		n := lib.Namespace{}
		n.Name = kn.Name
		n.Namespace = kn.Name
		n.Cluster = lib.Cfg.ClusterName
		// this one feels weird but to be consistent

		if hasOwnerAnnotation(kn, lib.Cfg.AnnotationFormatForEmails) == false &&
			hasOwnerAnnotation(kn, lib.Cfg.AnnotationFormatForChatIds) == false &&
			rules.IsNotIgnoredViolation(kn.Name, "namespace", violations.NO_OWNER_ANNOTATION_TYPE) {
			jsonString, err := json.Marshal(kn.Annotations)
			if err != nil {
				lib.Log.Error("Can not convert annotation to a valid json ", err)

			}
			n.Violations = append(n.Violations, violations.Violation{Source: string(jsonString), Type: violations.NO_OWNER_ANNOTATION_TYPE})
		}

		verifyRequiredAnnotations(kn.ObjectMeta.Annotations, &n.ViolatableEntity, "namespace", violations.REQUIRED_NAMESPACE_ANNOTATIONS_TYPE)
		verifyRequiredLabels(kn.ObjectMeta.Labels, &n.ViolatableEntity, "namespace", violations.REQUIRED_NAMESPACE_LABELS_TYPE)

		if len(n.ViolatableEntity.Violations) > 0 {
			allBadNamespaces = append(allBadNamespaces, n)
			if sendToKafka {
				lib.Log.Debug("Sending ", n.Name, " to kafka")
				err := KafkaProducer.SendData(lib.Cfg.KafkaActionTopic, kafka.NAMESPACE_MESSAGE, n)
				if err != nil {
					panic(err)
				}
			}
		}

	}
	metrics.Update(metrics.BAD_NAMESPACE_COUNT, len(allBadNamespaces))
	return allBadNamespaces
}

func hasOwnerAnnotation(namespace v1.Namespace, annotationKind string) bool {
	teamString, ok := namespace.Annotations[annotationKind]
	if ok {
		team := strings.Split(teamString, ",")
		if len(team) > 0 {
			return true
		}
	}
	return false
}

func verifyRequiredNamespaces(theNamespaces []v1.Namespace) []lib.Namespace {
	badNamespaces := []lib.Namespace{}

	for _, a := range lib.Cfg.RequiredEntities {
		rule := strings.Split(a, ":")

		// does the rule apply to this entity type?
		if !rules.Exact("namespace", rule[1]) {
			continue
		}

		found := false
		for _, kn := range theNamespaces {
			if rules.Exact(kn.ObjectMeta.Namespace, rule[0]) && rules.Exact("namespace", rule[1]) &&
				rules.Exact(kn.ObjectMeta.Name, rule[2]) {
				found = true
				break
			}
		}

		if !found {
			ns := lib.Namespace{}
			ns.Name = rule[2]
			ns.Cluster = lib.Cfg.ClusterName
			ns.Namespace = ns.Name
			ns.ViolatableEntity.Violations = append(ns.ViolatableEntity.Violations, violations.Violation{Source: rule[2], Type: violations.REQUIRED_NAMESPACES_TYPE})
			badNamespaces = append(badNamespaces, ns)
		}
	}

	return badNamespaces
}
