package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"pipeline/internal/db"
	"pipeline/internal/exporter"
	"pipeline/internal/importer"
	"pipeline/internal/merger"
)

var dbPath string

func main() {
	root := &cobra.Command{
		Use:   "pipeline",
		Short: "Brand Check UA data pipeline",
	}
	root.PersistentFlags().StringVar(&dbPath, "db", "pipeline.db", "SQLite database path")

	importCmd := &cobra.Command{Use: "import", Short: "Import raw data from a source"}
	importCmd.AddCommand(
		newImportOpenSanctionsCmd(),
		newImportKSECmd(),
		newImportBrandsCmd(),
	)

	root.AddCommand(importCmd, newMergeCmd(), newExportCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// openDB opens SQLite and ensures schema exists.
func openDB() (*sql.DB, error) {
	conn, err := db.Open(dbPath)
	if err != nil {
		return nil, err
	}
	if err := db.CreateSchema(conn); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func newImportOpenSanctionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "opensanctions",
		Short: "Import from OpenSanctions UA NSDC dataset",
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := openDB()
			if err != nil {
				return err
			}
			defer conn.Close()
			fmt.Println("importing OpenSanctions UA NSDC...")
			return importer.ImportOpenSanctions(conn)
		},
	}
}

func newImportKSECmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kse",
		Short: "Import from KSE Leave Russia dataset",
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := openDB()
			if err != nil {
				return err
			}
			defer conn.Close()
			fmt.Println("importing KSE Leave Russia...")
			return importer.ImportKSE(conn)
		},
	}
}

func newImportBrandsCmd() *cobra.Command {
	var brandsPath string
	cmd := &cobra.Command{
		Use:   "brands",
		Short: "Import brand→company mappings from local JSON and Open Food Facts",
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := openDB()
			if err != nil {
				return err
			}
			defer conn.Close()

			fmt.Printf("importing brands from %s...\n", brandsPath)
			if err := importer.ImportBrandsFromJSONPath(conn, brandsPath); err != nil {
				return fmt.Errorf("local brands: %w", err)
			}

			fmt.Println("importing brands from Open Food Facts (this may take several minutes)...")
			return importer.ImportBrandsFromOpenFoodFacts(conn)
		},
	}
	cmd.Flags().StringVar(&brandsPath, "brands", "data/brands/brands.json", "path to local brands JSON file")
	return cmd
}

func newMergeCmd() *cobra.Command {
	var overridesPath string
	cmd := &cobra.Command{
		Use:   "merge",
		Short: "Merge raw tables into companies table using fuzzy matching",
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := openDB()
			if err != nil {
				return err
			}
			defer conn.Close()

			var overrides []merger.Override
			data, err := os.ReadFile(overridesPath)
			if err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("read overrides %s: %w", overridesPath, err)
			}
			if err == nil {
				if err := json.Unmarshal(data, &overrides); err != nil {
					return fmt.Errorf("parse overrides %s: %w", overridesPath, err)
				}
			}

			fmt.Printf("merging with %d manual overrides...\n", len(overrides))
			return merger.Merge(conn, overrides)
		},
	}
	cmd.Flags().StringVar(&overridesPath, "overrides", "data/overrides.json", "path to overrides JSON file")
	return cmd
}

func newExportCmd() *cobra.Command {
	var outputDir string
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export companies table to companies.json.gz and version.json",
		RunE: func(cmd *cobra.Command, args []string) error {
			conn, err := openDB()
			if err != nil {
				return err
			}
			defer conn.Close()

			fmt.Printf("exporting to %s/...\n", outputDir)
			return exporter.Export(conn, outputDir)
		},
	}
	cmd.Flags().StringVar(&outputDir, "output", "output", "output directory for companies.json.gz")
	return cmd
}
