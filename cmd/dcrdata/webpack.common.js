const path = require('path')
const { CleanWebpackPlugin } = require('clean-webpack-plugin')
const MiniCssExtractPlugin = require('mini-css-extract-plugin')
const StyleLintPlugin = require('stylelint-webpack-plugin')
const Path = require("path");
const FileSystem = require("fs");

module.exports = {
  entry: {
    app: './public/index.js'
  },
  externals: {
    turbolinks: 'Turbolinks'
  },
  optimization: {
    chunkIds: 'natural',
    splitChunks: {
      chunks: 'all',
    }
  },
  target: "web",
  module: {
    rules: [
      {
        test: /\.s?[ac]ss$/,
        use: [
          MiniCssExtractPlugin.loader,
          {
            loader: 'css-loader',
            options: {
              modules: false, url: false, sourceMap: true
            }
          },
          {
            loader: 'sass-loader',
            options: {
              implementation: require("sass"), // dart-sass
              sourceMap: true
            }
          }
        ]
      }
    ]
  },
  plugins: [
    new CleanWebpackPlugin(),
    new MiniCssExtractPlugin({
      // Options similar to the same options in webpackOptions.output
      // both options are optional
      // filename: '[name].css',
      // chunkFilename: '[id].css'
      filename: 'css/style.[contenthash].css'
    }),
    new StyleLintPlugin({
      threads: true,
      allowEmptyInput: true, // avoid errors with .stylelintignore
    }),
    new class ReplaceBuildHash {
      apply(compiler) {
        compiler.hooks.done.tap('ReplaceBuildHash', (stats) => {
          const s = stats.toJson()
          if (s.errors.length) {
            // Do nothing on error
            return;
          }
          // Get filenames with hashes from app assets
          const hashes = s.entrypoints.app.assets.reduce((obj, v) => {
            if (v.name.includes('js/4')) obj.vendor = v.name.split('/')[1]
            if (v.name.includes('css/style')) obj.css = v.name.split('/')[1]
            if (v.name.includes('js/app')) obj.app = v.name.split('/')[1]
            return obj
          }, {})
          // Update js/css bundles with the generated cache hash on the
          // extras.tmpl file
          const tmplPath = Path.join(__dirname, './views/extras.tmpl')
          const tmplIn = FileSystem.readFileSync(tmplPath, "utf8")
          const tmplOut = tmplIn
            .replace(new RegExp('4.(\\w)\\w+\\.bundle\\.js'), `${hashes.vendor}`)
            .replace(new RegExp('style.(\\w)\\w+\\.css'), `${hashes.css}`)
            .replace(new RegExp('app.(\\w)\\w+\\.bundle\\.js'), `${hashes.app}`)
          FileSystem.writeFileSync(tmplPath, tmplOut)
        });
      }
    }
  ],
  output: {
    hashFunction: "xxhash64",
    filename: 'js/[name].[contenthash].bundle.js',
    path: path.resolve(__dirname, 'public/dist'),
    publicPath: '/dist/'
  },
  // Fixes weird issue with watch script. See
  // https://github.com/webpack/webpack/issues/2297#issuecomment-289291324
  watchOptions: {
    poll: true
  }
}
