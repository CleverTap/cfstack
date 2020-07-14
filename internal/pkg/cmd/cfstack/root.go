/*
Copyright © 2019 NAME HERE <EMAIL ADDRESS>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cfstack

import (
	goflag "flag"
	"fmt"
	"github.com/fatih/color"
	"github.com/golang/glog"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	flag "github.com/spf13/pflag"
	"github.com/spf13/viper"
	"os"
)

var cfgFile string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "cfstack",
	Short: "cloudformation cli with wings",
	Long: `cfstack allows to review and deploy your cloudformation changes
	using a manifest file. A manifest file lets u take a template file and 
	define it as a stack.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// For cobra + glog flags. Available to all subcommands.
		goflag.Parse()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stdout, color.RedString("❗️ %v\n", err))
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.cfstack.yaml)")
	rootCmd.PersistentFlags().StringP("profile", "", "", "Profile to use from AWS credentials")

	rootCmd.AddCommand(NewInitCmd())
	rootCmd.AddCommand(NewDeployCmd())
	rootCmd.AddCommand(NewDiffCmd())
	rootCmd.AddCommand(NewDeleteCmd())

	flag.CommandLine.AddGoFlagSet(goflag.CommandLine)
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		if err != nil {
			glog.Error(err)
			os.Exit(1)
		}

		// Search config in home directory with name ".cfstack" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigName(".cfstack")
	}

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		glog.Infof("Using config file: %s", viper.ConfigFileUsed())
	}
}

func exitWithError(command string, err error) {
	glog.Errorf("%v", err)
	glog.Errorf("Command %s has failed", command)
	os.Exit(1)
}
