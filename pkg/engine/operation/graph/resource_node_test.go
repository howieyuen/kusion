package graph

import (
	"context"
	"reflect"
	"sync"
	"testing"

	"bou.ke/monkey"
	"github.com/stretchr/testify/assert"

	"kusionstack.io/kusion/pkg/engine/models"
	opsmodels "kusionstack.io/kusion/pkg/engine/operation/models"
	"kusionstack.io/kusion/pkg/engine/runtime"
	"kusionstack.io/kusion/pkg/engine/runtime/kubernetes"
	"kusionstack.io/kusion/pkg/engine/states"
	"kusionstack.io/kusion/pkg/engine/states/local"
	"kusionstack.io/kusion/pkg/status"
	"kusionstack.io/kusion/third_party/terraform/dag"
)

func TestResourceNode_Execute(t *testing.T) {
	type fields struct {
		BaseNode baseNode
		Action   opsmodels.ActionType
		state    *models.Resource
	}
	type args struct {
		operation opsmodels.Operation
	}

	const Jack = "jack"
	const Pony = "pony"
	const Eric = "eric"
	mf := &models.Spec{Resources: []models.Resource{
		{
			ID:   Pony,
			Type: runtime.Kubernetes,
			Attributes: map[string]interface{}{
				"c": "d",
			},
			DependsOn: []string{Jack},
		},
		{
			ID:   Eric,
			Type: runtime.Kubernetes,
			Attributes: map[string]interface{}{
				"a": ImplicitRefPrefix + "jack.a.b",
			},
			DependsOn: []string{Pony},
		},
		{
			ID:   Jack,
			Type: runtime.Kubernetes,
			Attributes: map[string]interface{}{
				"a": map[string]interface{}{
					"b": "c",
				},
			},
			DependsOn: nil,
		},
	}}

	priorStateResourceIndex := map[string]*models.Resource{}
	for i, resource := range mf.Resources {
		priorStateResourceIndex[resource.ResourceKey()] = &mf.Resources[i]
	}

	newResourceState := &models.Resource{
		ID:   Eric,
		Type: runtime.Kubernetes,
		Attributes: map[string]interface{}{
			"a": ImplicitRefPrefix + "jack.a.b",
		},
		DependsOn: []string{Pony},
	}

	illegalResourceState := &models.Resource{
		ID:   Eric,
		Type: runtime.Kubernetes,
		Attributes: map[string]interface{}{
			"a": ImplicitRefPrefix + "jack.notExist",
		},
		DependsOn: []string{Pony},
	}

	graph := &dag.AcyclicGraph{}
	graph.Add(&RootNode{})

	tests := []struct {
		name   string
		fields fields
		args   args
		want   status.Status
	}{
		{
			name: "update",
			fields: fields{
				BaseNode: baseNode{ID: Jack},
				Action:   opsmodels.Update,
				state:    newResourceState,
			},
			args: args{operation: opsmodels.Operation{
				OperationType:           opsmodels.Apply,
				StateStorage:            local.NewFileSystemState(),
				CtxResourceIndex:        priorStateResourceIndex,
				PriorStateResourceIndex: priorStateResourceIndex,
				StateResourceIndex:      priorStateResourceIndex,
				IgnoreFields:            []string{"not_exist_field"},
				MsgCh:                   make(chan opsmodels.Message),
				ResultState:             states.NewState(),
				Lock:                    &sync.Mutex{},
				RuntimeMap:              map[models.Type]runtime.Runtime{runtime.Kubernetes: &kubernetes.KubernetesRuntime{}},
			}},
			want: nil,
		},
		{
			name: "delete",
			fields: fields{
				BaseNode: baseNode{ID: Jack},
				Action:   opsmodels.Delete,
				state:    newResourceState,
			},
			args: args{operation: opsmodels.Operation{
				OperationType:           opsmodels.Apply,
				StateStorage:            local.NewFileSystemState(),
				CtxResourceIndex:        priorStateResourceIndex,
				PriorStateResourceIndex: priorStateResourceIndex,
				StateResourceIndex:      priorStateResourceIndex,
				MsgCh:                   make(chan opsmodels.Message),
				ResultState:             states.NewState(),
				Lock:                    &sync.Mutex{},
				RuntimeMap:              map[models.Type]runtime.Runtime{runtime.Kubernetes: &kubernetes.KubernetesRuntime{}},
			}},
			want: nil,
		},
		{
			name: "illegalRef",
			fields: fields{
				BaseNode: baseNode{ID: Jack},
				Action:   opsmodels.Update,
				state:    illegalResourceState,
			},
			args: args{operation: opsmodels.Operation{
				OperationType:           opsmodels.Apply,
				StateStorage:            local.NewFileSystemState(),
				CtxResourceIndex:        priorStateResourceIndex,
				PriorStateResourceIndex: priorStateResourceIndex,
				StateResourceIndex:      priorStateResourceIndex,
				MsgCh:                   make(chan opsmodels.Message),
				ResultState:             states.NewState(),
				Lock:                    &sync.Mutex{},
				RuntimeMap:              map[models.Type]runtime.Runtime{runtime.Kubernetes: &kubernetes.KubernetesRuntime{}},
			}},
			want: status.NewErrorStatusWithMsg(status.IllegalManifest, "can't find specified value in resource:jack by ref:jack.notExist"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rn := &ResourceNode{
				baseNode: &tt.fields.BaseNode,
				Action:   tt.fields.Action,
				state:    tt.fields.state,
			}
			monkey.PatchInstanceMethod(reflect.TypeOf(tt.args.operation.RuntimeMap[runtime.Kubernetes]), "Apply",
				func(k *kubernetes.KubernetesRuntime, ctx context.Context, request *runtime.ApplyRequest) *runtime.ApplyResponse {
					mockState := *newResourceState
					mockState.Attributes["a"] = "c"
					return &runtime.ApplyResponse{
						Resource: &mockState,
					}
				})
			monkey.PatchInstanceMethod(reflect.TypeOf(tt.args.operation.RuntimeMap[runtime.Kubernetes]), "Delete",
				func(k *kubernetes.KubernetesRuntime, ctx context.Context, request *runtime.DeleteRequest) *runtime.DeleteResponse {
					return &runtime.DeleteResponse{Status: nil}
				})
			monkey.PatchInstanceMethod(reflect.TypeOf(tt.args.operation.RuntimeMap[runtime.Kubernetes]), "Read",
				func(k *kubernetes.KubernetesRuntime, ctx context.Context, request *runtime.ReadRequest) *runtime.ReadResponse {
					return &runtime.ReadResponse{Resource: request.PriorResource}
				})
			monkey.PatchInstanceMethod(reflect.TypeOf(tt.args.operation.StateStorage), "Apply",
				func(f *local.FileSystemState, state *states.State) error {
					return nil
				})
			defer monkey.UnpatchAll()

			assert.Equalf(t, tt.want, rn.Execute(&tt.args.operation), "Execute(%v)", tt.args.operation)
		})
	}
}

func Test_removeNestedField(t *testing.T) {
	t.Run("remove nested field", func(t *testing.T) {
		e1 := []interface{}{
			map[string]interface{}{"f": "f1", "g": "g1"},
		}
		e2 := []interface{}{
			map[string]interface{}{"f": "f2", "g": "g2"},
		}

		c := []interface{}{
			map[string]interface{}{"d": "d1", "e": e1},
			map[string]interface{}{"d": "d2", "e": e2},
		}

		a := map[string]interface{}{
			"b": 1,
			"c": c,
		}

		obj := map[string]interface{}{
			"a": a,
		}

		removeNestedField(obj, "a", "c", "e", "f")
		assert.Len(t, e1[0], 1)
		assert.Len(t, e2[0], 1)

		removeNestedField(obj, "a", "c", "e", "g")
		assert.Empty(t, e1[0])
		assert.Empty(t, e2[0])

		removeNestedField(obj, "a", "c", "e")
		assert.Len(t, c[0], 1)
		assert.Len(t, c[1], 1)

		removeNestedField(obj, "a", "c", "d")
		assert.Len(t, c[0], 0)
		assert.Len(t, c[1], 0)

		removeNestedField(obj, "a", "c")
		assert.Len(t, a, 1)

		removeNestedField(obj, "a", "b")
		assert.Len(t, a, 0)

		removeNestedField(obj, "a")
		assert.Empty(t, obj)
	})

	t.Run("remove spec.ports.targetPort", func(t *testing.T) {
		ports := []interface{}{
			map[string]interface{}{
				"port":       80,
				"protocol":   "TCP",
				"targetPort": 80,
			},
		}

		spec := map[string]interface{}{
			"clusterIP": "172.16.128.40",
			"ports":     ports,
		}

		obj := map[string]interface{}{
			"spec": spec,
		}

		removeNestedField(obj, "spec", "ports", "targetPort")
		assert.Len(t, ports[0], 2)
	})
}
