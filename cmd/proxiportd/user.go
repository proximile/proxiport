package main

import (
	"fmt"
	"log"

	"github.com/spf13/cobra"

	"github.com/proximile/proxiport/cmd/proxiportd/usercli"
	"github.com/proximile/proxiport/share/enums"
	"github.com/proximile/proxiport/share/logger"
)

const (
	UserAddCommand    = "add"
	UserDeleteCommand = "delete"
	UserChangeCommand = "change"
)

var (
	userCmd = &cobra.Command{
		Use:   "user",
		Short: "modify api users",
		Long:  "Add, change or delete api users",
	}
	userAddCmd = &cobra.Command{
		Use:     UserAddCommand,
		Short:   "add a new user",
		Long:    "Add a new user",
		Example: "proxiportd user add -u admin -g Administrators --2fa-sendto admin@example.com",
		Run: func(*cobra.Command, []string) {
			mLog := logger.NewMemLogger()
			err := decodeAndValidateConfig(&mLog)
			if err != nil {
				log.Fatalf("Invalid config: %v. See proxiportd --help", err)
			}

			userService, err := usercli.NewUserService(cfg)
			if err != nil {
				log.Fatal(err)
			}

			err = usercli.CreateUser(userService, usercli.NewPasswordReader(), *usernameFlag, *groupsFlag, *twoFASendToFlag)
			if err != nil {
				log.Fatal(err)
			}

			if userService.ProviderType() == enums.ProviderSourceFile {
				fmt.Println("You will need to restart the proxiportd server for changes to take effect.")
			}
		},
	}
	userDeleteCmd = &cobra.Command{
		Use:     UserDeleteCommand,
		Short:   "delete a user",
		Long:    "Delete a user",
		Example: "proxiportd user delete -u admin",
		Run: func(*cobra.Command, []string) {
			mLog := logger.NewMemLogger()
			err := decodeAndValidateConfig(&mLog)
			if err != nil {
				log.Fatalf("Invalid config: %v. See proxiportd --help", err)
			}

			userService, err := usercli.NewUserService(cfg)
			if err != nil {
				log.Fatal(err)
			}

			err = usercli.DeleteUser(userService, *usernameFlag)
			if err != nil {
				log.Fatal(err)
			}

			if userService.ProviderType() == enums.ProviderSourceFile {
				fmt.Println("You will need to restart the proxiportd server for changes to take effect.")
			}
		},
	}
	userChangeCmd = &cobra.Command{
		Use:     UserChangeCommand,
		Short:   "change a user",
		Example: "proxiportd user change -u admin -p",
		Long:    "Change a user",
		Run: func(*cobra.Command, []string) {
			mLog := logger.NewMemLogger()
			err := decodeAndValidateConfig(&mLog)
			if err != nil {
				log.Fatalf("Invalid config: %v. See proxiportd --help", err)
			}

			userService, err := usercli.NewUserService(cfg)
			if err != nil {
				log.Fatal(err)
			}

			err = usercli.UpdateUser(userService, usercli.NewPasswordReader(), *usernameFlag, *groupsFlag, *twoFASendToFlag, *passwordFlag)
			if err != nil {
				log.Fatal(err)
			}

			if userService.ProviderType() == enums.ProviderSourceFile {
				fmt.Println("You will need to restart the proxiportd server for changes to take effect.")
			}
		},
	}

	usernameFlag    *string
	twoFASendToFlag *string
	groupsFlag      *[]string
	passwordFlag    *bool
)

func init() {
	RootCmd.AddCommand(userCmd)

	userCmd.AddCommand(userAddCmd)
	userCmd.AddCommand(userDeleteCmd)
	userCmd.AddCommand(userChangeCmd)

	usernameFlag = userCmd.PersistentFlags().StringP("username", "u", "", "username [required]")
	err := userCmd.MarkPersistentFlagRequired("username")
	if err != nil {
		// This will return error if the flag doesn't exist, so it's ok to panic because it can only happen when changing the code
		panic(err)
	}

	groupsFlag = userAddCmd.Flags().StringSliceP("group", "g", nil, "group(s) user should be part of (comma separated)")
	twoFASendToFlag = userAddCmd.Flags().String("2fa-sendto", "", "email for 2fa")

	// add common flags from userAddCmd
	userChangeCmd.Flags().AddFlagSet(userAddCmd.Flags())

	passwordFlag = userChangeCmd.Flags().BoolP("password", "p", false, "update user's password")

	// reset default usage func
	userCmd.SetUsageFunc((&cobra.Command{}).UsageFunc())
}
