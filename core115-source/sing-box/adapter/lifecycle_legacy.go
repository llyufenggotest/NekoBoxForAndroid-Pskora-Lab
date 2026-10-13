package adapter

func LegacyStart(starter any, stage StartStage, scope *Scope) error {
	if lifecycle, isLifecycle := starter.(Lifecycle); isLifecycle {
		return lifecycle.Start(stage, scope)
	}
	switch stage {
	case StartStateInitialize:
		if preStarter, isPreStarter := starter.(interface {
			PreStart() error
		}); isPreStarter {
			return preStarter.PreStart()
		}
	case StartStateStart:
		if starter, isStarter := starter.(interface {
			Start() error
		}); isStarter {
			if err := starter.Start(); err != nil {
				return err
			}
			if closer, isCloser := starter.(interface{ Close() error }); isCloser {
				scope.Add(closer.Close)
			}
		}
	case StartStateStarted:
		if postStarter, isPostStarter := starter.(interface {
			PostStart() error
		}); isPostStarter {
			return postStarter.PostStart()
		}
	}
	return nil
}

type lifecycleServiceWrapper struct {
	SimpleLifecycle
	name string
}

func NewLifecycleService(service SimpleLifecycle, name string) LifecycleService {
	return &lifecycleServiceWrapper{
		SimpleLifecycle: service,
		name:            name,
	}
}

func (l *lifecycleServiceWrapper) Name() string {
	return l.name
}

func (l *lifecycleServiceWrapper) Start(stage StartStage, scope *Scope) error {
	return LegacyStart(l.SimpleLifecycle, stage, scope)
}
