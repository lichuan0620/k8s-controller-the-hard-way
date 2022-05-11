package main

import (
	goflag "flag"
	"fmt"
	"os"
	"runtime"
	"time"

	demo "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/client/clientset/versioned"
	demoinformers "github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/client/informers/externalversions"
	"github.com/lichuan0620/k8s-controller-the-hard-way/pkg/kubernetes/controllers"
	"github.com/lichuan0620/k8s-controller-the-hard-way/pkg/utils/signal"
	flag "github.com/spf13/pflag"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

var (
	kubeMasterURL  string
	kubeConfigPath string

	logger klog.Logger
)

func init() {
	klog.InitFlags(nil)
	flag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	flag.StringVar(&kubeMasterURL, "kube_master_url", "",
		"master URL used to construct the Kubernetes client")
	flag.StringVar(&kubeConfigPath, "kube_config_path", os.Getenv("KUBECONFIG"),
		"path to the KubeConfig file used to construct the Kubernetes client")

	flag.Usage = func() {
		_, _ = fmt.Fprintln(os.Stderr, "Usage of k8s-controller-demo:")
		flag.PrintDefaults()
	}
	flag.Parse()
	logger = klog.NewKlogr()
}

func main() {
	ctx := signal.SetupSignalContext()

	// setup kube client
	kubeConfig, err := clientcmd.BuildConfigFromFlags(kubeMasterURL, kubeConfigPath)
	if err != nil {
		logger.Error(err, "failed to build kubernetes rest config")
		os.Exit(1)
	}
	kubeConfig.QPS = 50
	kubeConfig.Burst = 100
	kubeConfig.Timeout = 10 * time.Second
	k8client, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		logger.Error(err, "failed to build native kubernetes client")
		os.Exit(1)
	}
	demoClient, err := demo.NewForConfig(kubeConfig)
	if err != nil {
		logger.Error(err, "failed to build demo.lichuan.guru kubernetes client")
		os.Exit(1)
	}

	// setup informers
	k8informerFactory := informers.NewSharedInformerFactory(k8client, 0)
	stsInf := k8informerFactory.Apps().V1().StatefulSets()
	svcInf := k8informerFactory.Core().V1().Services()
	demoInformerFactory := demoinformers.NewSharedInformerFactory(demoClient, 0)
	redisInf := demoInformerFactory.Demo().V1().Redises()
	tenantInf := demoInformerFactory.Demo().V1().Tenants()

	// setup controllers
	concurrency := (runtime.GOMAXPROCS(0) + 1) * 4
	reconcileTimeout := 10 * time.Second
	tenantController := controllers.NewTenantController(
		demoClient,
		redisInf.Lister(),
		tenantInf,
		concurrency,
		reconcileTimeout,
	)
	redisController := controllers.NewRedisController(
		k8client,
		demoClient,
		stsInf,
		svcInf,
		redisInf,
		tenantController.Enqueue,
		concurrency,
		reconcileTimeout,
	)

	// run informers
	logger.Info("starting resource informers")
	k8informerFactory.Start(ctx.Done())
	demoInformerFactory.Start(ctx.Done())

	// make sure informers are synced before joining leader election, because we don't want to
	// get elected unprepared
	logger.Info("waiting for informers to sync")
	cache.WaitForCacheSync(ctx.Done(),
		stsInf.Informer().HasSynced,
		svcInf.Informer().HasSynced,
		redisInf.Informer().HasSynced,
		tenantInf.Informer().HasSynced,
	)

	go tenantController.Run(ctx)
	go redisController.Run(ctx)

	logger.Info("all controllers running")
	<-ctx.Done()
}
