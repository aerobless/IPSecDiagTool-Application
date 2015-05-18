package main

import (
	"code.google.com/p/gopacket"
	"flag"
	"fmt"
	"github.com/ipsecdiagtool/ipsecdiagtool/capture"
	"github.com/ipsecdiagtool/ipsecdiagtool/config"
	"github.com/ipsecdiagtool/ipsecdiagtool/logging"
	"github.com/ipsecdiagtool/ipsecdiagtool/mtu"
	"github.com/ipsecdiagtool/ipsecdiagtool/packetloss"
	"github.com/kardianos/osext"
	"github.com/kardianos/service"
	"log"
	"os"
	"strconv"
)

var configuration config.Config
var capQuit chan bool
var icmpPackets = make(chan gopacket.Packet, 100)
var ipsecPackets = make(chan gopacket.Packet, 100)

// Program structures.
//  Define Start and Stop methods.
type program struct {
	exit chan struct{}
}

func (p *program) Start(s service.Service) error {
	p.exit = make(chan struct{})
	logging.InitLoger(configuration.SyslogServer, configuration.AlertCounter, configuration.AlertTime)

	if len(os.Args) > 1 {
		command := os.Args[1]
		switch command {
		case "interactive", "i":
			log.Println("Interactive testing")
			go p.run()
			if len(os.Args) > 2 {
				handleInteractiveArg(os.Args[2])
			} else {
				fmt.Println("Please specify an additional argument when using 'ipsecdiagtool " + command + "'")
				fmt.Println("Use 'ipsecdiagtool help' to get additional information.")
			}
		}
	} else {
		go packetloss.Detectnew(configuration, ipsecPackets, false)
		go p.run()

		//TODO: Remove before release.
		if configuration.Debug {
			//Code tested directly in the IDE belongs in here
		}
	}
	return nil
}

func handleInteractiveArg(arg string) {
	switch arg {
	case "mtu", "m":
		go mtu.FindAll()
	case "packetloss", "p", "pl":
		if len(os.Args) > 3 {
			pcapPath := os.Args[3]
			configuration.PcapFile = pcapPath
			log.Println("Reading packetloss test data from file.")
			go packetloss.Detectnew(configuration, ipsecPackets, true)
		}
		if configuration.PcapFile == "" {
			log.Println("Detecting packetloss from ethernet")
		} else {
			log.Println("Detecting packetloss from configured file:", configuration.PcapFile)
		}
		go packetloss.Detectnew(configuration, ipsecPackets, true)
	default:
		fmt.Println("Command", arg, "not recognized")
		fmt.Println("See 'ipsecdiagtool help' for additional information.")
	}
}

func (p *program) run() error {
	if configuration.Debug {
		log.Println("Running Daemon via", service.Platform())
	}
	mtu.Init(configuration, icmpPackets)
	capQuit = capture.Start(configuration, icmpPackets, ipsecPackets)

	<-p.exit
	return nil
}

func chooseService(action string) {
	fmt.Println("The following services are supported on this system:")
	services := service.AvailableSystems()
	var space = "  "
	for serv := range services {
		fmt.Println(space + strconv.Itoa(serv) + ". " + services[serv].String())
	}
	i := 0
	for i == 0 {
		fmt.Println("Enter the number of the service you wish to " + action)
		var input string
		fmt.Scan(&input)
		var err error
		i, err = strconv.Atoi(input)
		if err != nil {
			fmt.Println("Please enter a valid integer.")
		} else if i > (len(services)-1) || i < 0 {
			fmt.Println("The number you chose is out of range.")
			i = 0
		} else {
			service.ChooseSystem(services[i])
			log.Println("You have chosen", service.ChosenSystem())
		}
	}
}

func installService(s service.Service) {
	err := s.Install()
	if err != nil {
		log.Println(err)
	} else {
		log.Println("IPSecDiagTool Daemon successfully installed.")
	}
}

func uninstallService(s service.Service) {
	err := s.Uninstall()
	if err != nil {
		log.Println(err)
	} else {
		log.Println("IPSecDiagTool Daemon successfully uninstalled.")
	}
}

func printAbout() {
	fmt.Print("IPSecDiagTool is being developed at HSR (Hoschschule für Technik Rapperswil)\n" +
		"as a semester/bachelor thesis. For more information please visit our repository on\n \n" +
		"Authors: Jan Balmer, Theo Winter \n" +
		"Github: https://github.com/IPSecDiagTool/IPSecDiagTool\n")
}

func printDebug(conf config.Config) {
	fmt.Println("IPSecDiagTool Debug Information")
	fmt.Println(conf.ToString())
}

func printHelp() {
	var spac = "\n   "
	var help = "Usage: ipsecdiagtool [OPTION ...] \n" +
		"IPSecDiagTool detects packet loss for all connected IPSec VPN tunnels. It can also discover the MTU for all configured connections. " +
		"IPSecDiagTool is intended to be run as a daemon on both sides of a VPN tunnel." + spac +
		"\n" +
		"Daemon operation mode:" + spac +
		"ipsecdiagtool install    #Install the daemon/service on your system." + spac +
		"ipsecdiagtool uninstall  #Uninstall the daemon/service from system." + spac +
		"ipsecdiagtool mtu        #Tell a locally running daemon to start discoverying the MTU." + spac +
		"\n" +
		"Interactive opertation mode:" + spac +
		"ipsecdiagtool i mtu        #Run the mtu discovery locally, without a daemon." + spac +
		"ipsecdiagtool i pl         #Run the packetloss detection locally, without a daemon." + spac +
		"ipsecdiagtool i pl [path]  #Run the packetloss detection locally, reading pcap data from a file." + spac +
		"\n" +
		"Information commands:" + spac +
		"ipsecdiagtool debug        #Show debug information." + spac +
		"ipsecdiagtool help         #Display this help menu." + spac +
		"ipsecdiagtool about        #Who created this application."
	fmt.Println(help)
}

func (p *program) Stop(s service.Service) error {
	// Any work in Stop should be quick, usually a few seconds at most.
	log.Println("Stopping IPSecDiagTool")
	close(p.exit)
	return nil
}

// Service setup.
//   Define service config.
//   Create the service.
//   Setup the logger.
//   Handle service controls (optional).
//   Run the service.
func main() {
	//Load configuration
	path, err := osext.ExecutableFolder()
	check(err)
	configuration = config.LoadConfig(path)

	//Check args for installation, needs to be done before the service is started.
	if len(os.Args) > 1 {
		command := os.Args[1]
		switch command {
		case "install":
			chooseService("install")
			s := initService()
			installService(s)
		case "uninstall", "remove":
			chooseService("uninstall")
			s := initService()
			uninstallService(s)
		case "interactive", "i":
			s := initService()
			err = s.Run()
		case "mtu-discovery", "mtu":
			mtu.RequestDaemonMTU(configuration.ApplicationID)
			log.Println("The daemon was triggered to start MTU Discovery for all configured tunnels.")
		case "about", "a":
			printAbout()
		case "debug", "d":
			printDebug(configuration)
		case "help", "--help", "h":
			printHelp()
		default:
			fmt.Println("Argument not reconized. Run 'ipsecdiagtool help' to learn how to use this application.")
		}
	} else {
		s := initService()
		err = s.Run()
	}
	check(err)
}

func initService() service.Service {
	svcFlag := flag.String("service", "", "Control the system service.")
	flag.Parse()

	svcConfig := &service.Config{
		Name:        "ipsecdiagtool",
		DisplayName: "A service for IPSecDiagTool",
		Description: "Detects packet loss & periodically reports the MTU for all configured tunnels.",
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal(err)
	}
	errs := make(chan error, 5)
	//logger, err = s.Logger(errs)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		for {
			err := <-errs
			check(err)
		}
	}()

	if len(*svcFlag) != 0 {
		err := service.Control(s, *svcFlag)
		if err != nil {
			log.Printf("Valid actions: %q\n", service.ControlAction)
			log.Fatal(err)
		}
		os.Exit(0)
	}
	return s
}

func check(e error) {
	if e != nil {
		panic(e)
	}
}
