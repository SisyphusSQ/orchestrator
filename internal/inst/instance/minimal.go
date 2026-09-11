package instance

type MinimalInstance struct {
	Key         InstanceKey
	MasterKey   InstanceKey
	ClusterName string
}

func (instance *MinimalInstance) ToInstance() *Instance {
	return &Instance{
		Key:         instance.Key,
		MasterKey:   instance.MasterKey,
		ClusterName: instance.ClusterName,
	}
}
