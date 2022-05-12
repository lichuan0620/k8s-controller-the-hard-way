package main

import (
	"context"
	goflag "flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"runtime"
	"time"

	demov1 "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/apis/demo/v1"
	"github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/client"
	"github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/metrics"
	"github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/reconcilers"
	"github.com/lichuan0620/k8s-controller-the-hard-way/pkg/utils/controller"
	leaderelectionutil "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/utils/leaderelection"
	"github.com/lichuan0620/k8s-controller-the-hard-way/pkg/utils/signal"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	flag "github.com/spf13/pflag"
	"golang.org/x/sync/errgroup"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

var (
	kubeMasterURL  string
	kubeConfigPath string

	leaderElectionLockNamespace string
	leaderElectionLockLeaseName string
	leaderElectionLockType      string
	leaderElectionLeaseDuration time.Duration
	leaderElectionRenewDeadline time.Duration
	leaderElectionRetryPeriod   time.Duration

	telemetryListenAddress string

	logger klog.Logger
)

func init() {
	klog.InitFlags(nil)
	flag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	flag.StringVar(&kubeMasterURL, "kube_master_url", "",
		"master URL used to construct the Kubernetes client")
	flag.StringVar(&kubeConfigPath, "kube_config_path", os.Getenv("KUBECONFIG"),
		"path to the KubeConfig file used to construct the Kubernetes client")
	flag.StringVar(&leaderElectionLockType, "leader_election_lock_type", resourcelock.ConfigMapsLeasesResourceLock,
		"the type of the leader election resource lock object")
	flag.StringVar(&leaderElectionLockNamespace, "leader_election_lock_namespace", mustGetCurrentNamespace(),
		"namespace where the resource lock will be created")
	flag.StringVar(&leaderElectionLockLeaseName, "leader_election_lock_lease_name", "demo-controller-leader-lock",
		"name of the resource lock object")
	flag.DurationVar(&leaderElectionLeaseDuration, "leader_election_lease_duration", 15*time.Second,
		"the duration that non-leader candidates will wait to force acquire leadership")
	flag.DurationVar(&leaderElectionRenewDeadline, "leader_election_renew_deadline", 10*time.Second,
		"the duration that the acting control plane will retry refreshing leadership before giving up")
	flag.DurationVar(&leaderElectionRetryPeriod, "leader_election_retry_period", 2*time.Second,
		"the duration the LeaderElector clients should wait between tries of actions")
	flag.StringVar(&telemetryListenAddress, "telemetry_listen_address", "0.0.0.0:9090",
		"address to listen to for metrics and healthz API")

	flag.Usage = func() {
		_, _ = fmt.Fprintln(os.Stderr, "Usage of k8s-controller-demo:")
		flag.PrintDefaults()
	}
	flag.Parse()
	logger = klog.NewKlogr()
}

func main() {
	// setup subroutine management with graceful exit
	runErrGroup, runCtx := errgroup.WithContext(signal.SetupSignalContext())

	// setup kube client
	kubeConfig, err := clientcmd.BuildConfigFromFlags(kubeMasterURL, kubeConfigPath)
	if err != nil {
		logger.Error(err, "failed to build kubernetes rest config")
		os.Exit(1)
	}
	kubeConfig.QPS = 50
	kubeConfig.Burst = 100
	kubeConfig.Timeout = 10 * time.Second

	k8client, informers, err := client.New(kubeConfig, nil)
	if err != nil {
		logger.Error(err, "failed to build kubernetes client")
		os.Exit(1)
	}

	// setup controllers
	reconcileTimeout := 10 * time.Second
	maxOPS := 10.
	gomaxprocs := runtime.GOMAXPROCS(0)
	tenantController, err := controller.NewBasicController(controller.BasicTypeOptions{
		Options: controller.Options{
			Informers: informers,
			Resource:  &demov1.Tenant{},
			Reconciler: reconcilers.NewTenantReconciler(
				k8client, reconcileTimeout,
			),
			Predicate:    controller.ResourceVersionPredicate(),
			ResyncPeriod: pointer.Duration(0), // disable resync
			MaxWorkerOPS: maxOPS,
		},
		ReconcileTimeout: reconcileTimeout,
		Concurrency:      gomaxprocs * 10,
	})
	if err != nil {
		logger.Error(err, "failed to build Tenant controller")
		os.Exit(1)
	}
	redisGVK, err := apiutil.GVKForObject(&demov1.Redis{}, client.Scheme)
	if err != nil {
		logger.Error(err, "parse Redis GVK")
		os.Exit(1)
	}
	redisController, err := controller.NewBasicController(controller.BasicTypeOptions{
		Options: controller.Options{
			Informers: informers,
			Resource:  &demov1.Redis{},
			Reconciler: reconcilers.NewRedisReconciler(
				tenantController.Enqueue, k8client, reconcileTimeout,
			),
			Predicate:    controller.ResourceVersionPredicate(),
			ResyncPeriod: pointer.Duration(0), // disable resync
			MaxWorkerOPS: maxOPS,
			WatchOptions: []controller.WatchOption{
				{
					Resource: &appsv1.StatefulSet{},
					Predicate: controller.Predicates{
						controller.OwnerPredicate(redisGVK),
						controller.ResourceVersionPredicate(),
					},
					GetKeyFunc:   reconcilers.ParseRedisKeyFromObj,
					ResyncPeriod: pointer.Duration(0), // disable resync
				},
				{
					Resource: &corev1.Service{},
					Predicate: controller.Predicates{
						controller.OwnerPredicate(redisGVK),
						controller.ResourceVersionPredicate(),
					},
					GetKeyFunc:   reconcilers.ParseRedisKeyFromObj,
					ResyncPeriod: pointer.Duration(0), // disable resync
				},
			},
		},
		ReconcileTimeout: reconcileTimeout,
		Concurrency:      gomaxprocs * 10,
	})
	if err != nil {
		logger.Error(err, "failed to build Redis controller")
		os.Exit(1)
	}

	// setup leader election
	rsrcLock, err := leaderelectionutil.NewResourceLock(
		kubeConfig,
		leaderElectionLockType,
		leaderElectionLockNamespace,
		leaderElectionLockLeaseName,
	)
	if err != nil {
		logger.Error(err, "failed to build leader election lease lock resource")
		os.Exit(1)
	}
	lostLeaderSignal := make(chan struct{})
	leaderHealthz := leaderelection.NewLeaderHealthzAdaptor(10 * time.Second)
	elector, err := leaderelection.NewLeaderElector(leaderelection.LeaderElectionConfig{
		Lock:          rsrcLock,
		LeaseDuration: leaderElectionLeaseDuration,
		RenewDeadline: leaderElectionRenewDeadline,
		RetryPeriod:   leaderElectionRetryPeriod,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				logger.Info("gained leadership")
				runErrGroup.Go(func() error {
					redisController.Run(runCtx)
					return nil
				})
				runErrGroup.Go(func() error {
					tenantController.Run(runCtx)
					return nil
				})
				runErrGroup.Go(func() error {
					select {
					case <-runCtx.Done():
						return nil
					case <-lostLeaderSignal:
						return errors.New("lost leadership")
					}
				})
			},
			OnStoppedLeading: func() {
				logger.Info("lost leadership")
				close(lostLeaderSignal)
			},
			OnNewLeader: func(identity string) {
				if rsrcLock.Identity() != identity {
					logger.Info("new leader observed", "identity", identity)
				}
			},
		},
		WatchDog:        leaderHealthz,
		ReleaseOnCancel: true,
	})
	if err != nil {
		logger.Error(err, "failed to build leader elector")
		os.Exit(1)
	}
	leaderHealthz.SetLeaderElection(elector)

	// setup telemetry server
	telemetryServerMux := http.NewServeMux()
	telemetryServerMux.HandleFunc("/healthz", func(writer http.ResponseWriter, request *http.Request) {
		if err := leaderHealthz.Check(request); err != nil {
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = writer.Write([]byte(err.Error()))
			return
		}
		if !informers.WaitForCacheSync(request.Context()) {
			writer.WriteHeader(http.StatusInternalServerError)
			_, _ = writer.Write([]byte("informers not synced"))
			return
		}
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("OK"))
	})
	metrics.InstallDefault()
	telemetryServerMux.Handle("/metrics", promhttp.Handler())
	telemetryServer := http.Server{
		Addr:              telemetryListenAddress,
		Handler:           telemetryServerMux,
		IdleTimeout:       time.Minute,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		ReadHeaderTimeout: time.Second,
	}

	// run informers
	logger.Info("starting resource informers")
	runErrGroup.Go(func() error {
		return errors.Wrap(informers.Start(runCtx), "start informers")
	})

	// run telemetry server
	runErrGroup.Go(func() error {
		logger.Info("listen and serve telemetry service", "address", telemetryListenAddress)
		if err := telemetryServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return errors.Wrap(err, "serve telemetry service")
		}
		return nil
	})

	// make sure informers are synced before joining leader election, because we don't want to
	// get elected unprepared
	logger.Info("waiting for informers to sync")
	if !informers.WaitForCacheSync(runCtx) {
		logger.Error(errors.New("informers failed to sync"), "")
		os.Exit(1)
	}
	logger.Info("joining leader election")
	elector.Run(runCtx)
}

func mustGetCurrentNamespace() string {
	ret := os.Getenv("POD_NAMESPACE")
	if bytes, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace"); err == nil {
		ret = string(bytes)
	}
	return ret
}
