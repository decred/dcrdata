const path = require('path')
const webpack = require('webpack')

// var LiveReloadPlugin = require('webpack-livereload-plugin')
const CleanWebpackPlugin = require('clean-webpack-plugin')

module.exports = {
  'mode': 'development',
  // 'mode': 'production',
  devtool: 'inline-source-map',
  entry: {
    index: './public/js/index.js'
  },
  // devServer: {
  //   contentBase: 'public/js/dist',
  //   port: 7777,
  //   hot: true
  // },
  optimization: {
    usedExports: true
  },
  plugins: [
    new CleanWebpackPlugin(['./public/js/dist'])
  ],
  module: {
    rules: [
      {
        test: /\.m?js$/,
        exclude: /(node_modules|bower_components)/,
        use: {
          loader: 'babel-loader',
          options: {
            presets: ['@babel/preset-env']
          }
        }
      },
      {
        test: /\.js$/,
        exclude: /node_modules/,
        loader: 'eslint-loader',
        options: {
          formatter: require('eslint/lib/formatters/stylish')
          // fix: true
        }
      }
    ]
  },
  output: {
    chunkFilename: '[name].bundle.js',
    path: path.resolve(__dirname, 'public/js/dist')
  }

}
