export interface RcrainfoAccountProfileInput {
  /**
   * Path to the input CSV file.
   */
  input_file: string;
  /**
   * Path to the output directory.
   */
  output_dir: string;
  run_rcra?: boolean;
  run_frs?: boolean;
}
